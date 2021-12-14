// Copyright 2020-2021 JackTrip Labs, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/xthexder/go-jack"
)

const (
	FileDuration   = 10
	FileCountLimit = 10
	NumChannels    = 2
	// Changing this will also involve changes in the process handler
	BitDepth = 16
	MaxBit   = math.MaxInt16
)

var (
	AudioInPorts   []*jack.Port
	AudioFilenames []string
	FrameBuffer    []frame.Frame
	HLSPlaylist    *m3u8.MediaPlaylist
	JackSampleRate int
	JackBufferSize int
	fhLock         sync.Mutex
	encoder        *flac.Encoder
)

// Recorder listens to audio and records it to disk
type Recorder struct {
	Name       string
	JackClient *jack.Client
	ClientLock sync.Mutex
	Shutdown   chan struct{}
}

// NewRecorder constructs a new instance of Recorder
// TODO: Should we merge this and make it part of autoconnector?
//       Is there any benefit to having 2 JACK clients vs 1?
//       There's a lot of shared code between them
func NewRecorder() *Recorder {
	return &Recorder{
		Name:     "recorder",
		Shutdown: make(chan struct{}),
	}
}

func openFLAC() {
	fhLock.Lock()
	defer fhLock.Unlock()
	if JackSampleRate <= 0 {
		return
	}
	// TODO: Make filename secret-like
	now := time.Now().Unix()
	name := fmt.Sprintf("%s/test-%d.flac", MediaDir, now)
	fh, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	// Keep track of files created and rotate files
	AudioFilenames = append(AudioFilenames, name)
	if len(AudioFilenames) >= FileCountLimit {
		trash := AudioFilenames[0]
		trashMP3 := pathutil.TrimExt(trash) + ".mp3"
		AudioFilenames = AudioFilenames[1:]
		os.RemoveAll(trash)
		os.RemoveAll(trashMP3)
	}
	info := &meta.StreamInfo{
		BlockSizeMin:  16,
		BlockSizeMax:  65535,
		SampleRate:    uint32(JackSampleRate),
		NChannels:     NumChannels,
		BitsPerSample: BitDepth,
	}
	encoder, err = flac.NewEncoder(fh, info)
}

func closeFLAC() {
	fhLock.Lock()
	defer fhLock.Unlock()
	if encoder != nil {
		encoder.Close()
	}
}

func updateHLSPlaylist() {
	if HLSPlaylist != nil {
		orig := AudioFilenames[len(AudioFilenames)-1]
		converted := pathutil.TrimExt(orig) + ".mp3"
		basename := filepath.Base(converted)
		// TODO: Is there a better way to do the conversion?
		cmd := exec.Command("ffmpeg", "-i", orig, "-q:a", "0", converted)
		cmd.Run()
		// TODO: Slide vs Append? IDK what's better yet
		HLSPlaylist.Slide(basename, FileDuration, "")
		dest := fmt.Sprintf("%s/playlist.m3u8", MediaDir)
		os.WriteFile(dest, HLSPlaylist.Encode().Bytes(), 0644)
	}
}

func flush(frameBuffer []frame.Frame) {
	if len(frameBuffer) > 0 {
		openFLAC()
		for _, frame := range frameBuffer {
			if err := encoder.WriteFrame(&frame); err != nil {
				fmt.Println(err)
				return
			}
		}
		closeFLAC()
		updateHLSPlaylist()
	}
}

// processBuffer reads frames from the port's buffer
func processBuffer(nframes uint32) int {
	// JackBufferSize is global in order for the process callback to access it - check if it's been set otherwise there will be no data
	if JackBufferSize <= 0 || JackSampleRate <= 0 {
		return 0
	}
	if len(FrameBuffer) >= JackSampleRate*FileDuration/JackBufferSize {
		go flush(FrameBuffer)
		FrameBuffer = []frame.Frame{}
	}
	// Initialize new subframe/channel
	subframes := make([]*frame.Subframe, NumChannels)
	for i := range subframes {
		subframe := &frame.Subframe{
			Samples: make([]int32, JackBufferSize),
		}
		subframes[i] = subframe
	}
	// Dump raw sample data into respective subframes
	for i, port := range AudioInPorts {
		subHdr := frame.SubHeader{
			Pred:   frame.PredVerbatim,
			Order:  0,
			Wasted: 0,
		}
		subframes[i].SubHeader = subHdr
		subframes[i].NSamples = JackBufferSize
		subframes[i].Samples = subframes[i].Samples[:JackBufferSize]
		samples := port.GetBuffer(nframes)
		for j, sample := range samples {
			subframes[i].Samples[j] = int32(uint16(sample * MaxBit))
		}
	}
	// Package buffer of samples into frame
	header := frame.Header{
		HasFixedBlockSize: false,
		BlockSize:         uint16(JackBufferSize),
		SampleRate:        uint32(JackSampleRate),
		Channels:          frame.ChannelsLR,
		BitsPerSample:     BitDepth,
	}
	frame := &frame.Frame{
		Header:    header,
		Subframes: subframes,
	}
	FrameBuffer = append(FrameBuffer, *frame)
	return 0
}

// onShutdown only runs when upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.JackClient = nil
	AudioInPorts = nil
	JackSampleRate = 0
	JackBufferSize = 0
	AudioFilenames = nil
	closeFLAC()
}

// TeardownClient closes the currently active JACK client
func (r *Recorder) TeardownClient() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	if r.JackClient != nil {
		r.JackClient.Close()
	}
	r.JackClient = nil
	AudioInPorts = nil
	JackSampleRate = 0
	JackBufferSize = 0
	AudioFilenames = nil
	closeFLAC()
	log.Info("Teardown of JACK client completed")
}

// SetupClient establishes a new client to listen in on JACK ports
func (r *Recorder) SetupClient() {
	var err error
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	err = waitForDaemon()
	if err != nil {
		panic(err)
	}
	portRegistrationFunc := func(client *jack.Client) {
		for i := 1; i <= NumChannels; i++ {
			portName := fmt.Sprintf("send_%d", i)
			portIn := client.PortRegister(portName, jack.DEFAULT_AUDIO_TYPE, jack.PortIsInput, 0)
			AudioInPorts = append(AudioInPorts, portIn)
		}
	}
	client, err := initClient(r.Name, nil, r.onShutdown, processBuffer, portRegistrationFunc, false)
	if err != nil {
		panic(err)
	}
	r.JackClient = client
	JackSampleRate = int(r.JackClient.GetSampleRate())
	JackBufferSize = int(r.JackClient.GetBufferSize())
	HLSPlaylist, err = m3u8.NewMediaPlaylist(10, 20)
	if err != nil {
		panic(err)
	}
	log.Info("Setup of JACK client completed", "name", r.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (r *Recorder) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		_, ok := <-r.Shutdown
		if !ok {
			log.Info("Shutdown channel is closed")
			return
		}
	}
}
