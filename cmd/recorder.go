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
	"strconv"
	"strings"
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
	// FileDuration is the duration (in seconds) of each audio segment file
	FileDuration = 5
	// FileCountLimit is the maximum number of audio segment files kept on disk
	FileCountLimit = 10
	// NumRecorderChannels is the number of input channels of the recorder
	NumRecorderChannels = 2
	// BitDepth is the bit-resolution used when encoding audio data - changing this involves changes in processBuffer()
	BitDepth = 16
	// HLSIndex is the top-level HLS metadata file
	HLSIndex = "index.m3u8"
)

var (
	// AudioFilenames is an in-memory array of filenames used to perform rotation
	AudioFilenames []string
	// FrameBuffer is an in-memory buffer of FLAC frames
	FrameBuffer []frame.Frame
	// HLSMasterPlaylist is the top-level HLS playlist struct
	HLSMasterPlaylist *m3u8.MasterPlaylist
)

// Recorder listens to audio and records it to disk
type Recorder struct {
	// Name will be the JACK client name
	Name string
	// JackBufferSize is the buffer size determined at the start of client initiation
	JackBufferSize int
	// JackSampleRate is the sample rate determined at the start of client initiation
	JackSampleRate int
	// JackClient is the client interacting with jackd
	JackClient *jack.Client
	// RecorderPorts are the input ports used to scrape audio
	RecorderPorts []*jack.Port
	// ClientLock is a mutex used for client-daemon interactions
	ClientLock sync.Mutex
	// RawSamplesChan is the channel were raw audio samples are passed through
	RawSamplesChan chan [][]jack.AudioSample
}

// NewRecorder constructs a new instance of Recorder
// TODO: Should we merge this and make it part of autoconnector?
//       Is there any benefit to having 2 JACK clients vs 1?
//       There's some shared code between them
func NewRecorder() *Recorder {
	return &Recorder{
		Name:           "recorder",
		RawSamplesChan: make(chan [][]jack.AudioSample, 500),
	}
}

// processBuffer reads frames from each port's buffer
func (r *Recorder) processBuffer(nframes uint32) int {
	// Ignore raw audio samples if client initiation is still in progress
	if r.JackBufferSize <= 0 || r.JackSampleRate <= 0 {
		return 0
	}
	// Read audio data from ports and immediately send to the channel for further processing
	raw := [][]jack.AudioSample{}
	for _, port := range r.RecorderPorts {
		samples := port.GetBuffer(nframes)
		raw = append(raw, samples)
	}
	r.RawSamplesChan <- raw
	return 0
}

// reset nullifies all things recording-related in a thread-unsafe manner - used when jackd/jacktrip restarts
func (r *Recorder) reset() {
	r.JackClient, r.RecorderPorts, r.JackSampleRate, r.JackBufferSize = nil, nil, 0, 0
	for _, trash := range AudioFilenames {
		cleanStaleFile(trash)
	}
	cleanStaleFile(filepath.Join(MediaDir, HLSIndex))
	cleanStaleFile(filepath.Join(MediaDir, "init.mp4"))
	cleanStaleFile(filepath.Join(MediaDir, "playlist-*.m3u8"))
	AudioFilenames, FrameBuffer = nil, nil
	log.Info("Teardown of JACK client completed")
}

// onShutdown only runs upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.reset()
}

// TeardownClient closes the currently active JACK client
func (r *Recorder) TeardownClient() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	if r.JackClient != nil {
		r.JackClient.Close()
	}
	r.reset()
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
		for i := 1; i <= NumRecorderChannels; i++ {
			portName := fmt.Sprintf("send_%d", i)
			portIn := client.PortRegister(portName, jack.DEFAULT_AUDIO_TYPE, jack.PortIsInput, 0)
			r.RecorderPorts = append(r.RecorderPorts, portIn)
		}
	}
	client, err := initClient(r.Name, nil, r.onShutdown, r.processBuffer, portRegistrationFunc, false)
	if err != nil {
		panic(err)
	}
	r.JackClient = client
	r.JackSampleRate = int(r.JackClient.GetSampleRate())
	r.JackBufferSize = int(r.JackClient.GetBufferSize())
	HLSMasterPlaylist = m3u8.NewMasterPlaylist()
	HLSMasterPlaylist.SetVersion(7)
	log.Info("Setup of JACK client completed", "name", r.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (r *Recorder) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		raw, ok := <-r.RawSamplesChan
		if !ok {
			log.Info("Raw samples channel is closed")
			return
		}
		r.addFrame(raw)
	}
}

func (r *Recorder) addFrame(audioSamples [][]jack.AudioSample) {
	sampleRate, bufferSize := r.JackSampleRate, r.JackBufferSize
	if sampleRate <= 0 || bufferSize <= 0 {
		return
	}
	if len(FrameBuffer) >= sampleRate*FileDuration/bufferSize {
		go flush(FrameBuffer, sampleRate)
		FrameBuffer = []frame.Frame{}
	}
	subframes := make([]*frame.Subframe, NumRecorderChannels)
	for i := range subframes {
		subframe := &frame.Subframe{
			Samples: make([]int32, bufferSize),
		}
		subframes[i] = subframe
	}
	// Dump raw sample data into respective subframes
	for i, samples := range audioSamples {
		subHdr := frame.SubHeader{
			Pred:   frame.PredVerbatim,
			Order:  0,
			Wasted: 0,
		}
		subframes[i].SubHeader = subHdr
		subframes[i].NSamples = bufferSize
		subframes[i].Samples = subframes[i].Samples[:bufferSize]
		for j, sample := range samples {
			subframes[i].Samples[j] = int32(uint16(sample * math.MaxInt16))
		}
	}
	// Package subframes into frame using stereo settings
	header := frame.Header{
		HasFixedBlockSize: false,
		BlockSize:         uint16(bufferSize),
		SampleRate:        uint32(sampleRate),
		Channels:          frame.ChannelsLR,
		BitsPerSample:     BitDepth,
	}
	frame := &frame.Frame{Header: header, Subframes: subframes}
	FrameBuffer = append(FrameBuffer, *frame)
}

// createNewFLAC creates a new FLAC file based on server ID and current time
func createNewFLAC() (*os.File, error) {
	name := fmt.Sprintf("%s/raw-%s-%d.flac", MediaDir, os.Getenv("JACKTRIP_SERVER_ID"), time.Now().Unix())
	fh, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return fh, nil
}

// openFLAC creates a FLAC encoder to write audio samples to a file
func openFLAC(sampleRate int) (*flac.Encoder, error) {
	fh, err := createNewFLAC()
	if err != nil {
		return nil, err
	}
	// Keep track of files created and rotate files
	AudioFilenames = append(AudioFilenames, fh.Name())
	if len(AudioFilenames) > FileCountLimit {
		cleanStaleFile(AudioFilenames[0])
		AudioFilenames = AudioFilenames[1:]
	}
	info := &meta.StreamInfo{
		BlockSizeMin:  16,
		BlockSizeMax:  65535,
		SampleRate:    uint32(sampleRate),
		NChannels:     NumRecorderChannels,
		BitsPerSample: BitDepth,
	}
	return flac.NewEncoder(fh, info)
}

// cleanStaleFile deletes all files that pattern-match the input filename (minus extension)
func cleanStaleFile(filename string) {
	prefix := pathutil.TrimExt(filename)
	files, _ := filepath.Glob(prefix + "*")
	for _, f := range files {
		os.Remove(f)
	}
}

func transcode(inputFile, encoding, bitrate string, index int) {
	basename := filepath.Base(inputFile)
	basenameWithoutExt := strings.TrimSuffix(basename, filepath.Ext(basename))
	outputFile := fmt.Sprintf("%s/%s-%s-%%03d.m4s", MediaDir, basenameWithoutExt, bitrate)
	cmd := exec.Command(
		// Call ffmpeg on the most-recently created FLAC file
		"ffmpeg", "-hide_banner", "-i", inputFile,
		// Convert original to desired encoding + bitrate
		"-map", "0:a", "-c:a:0", encoding, "-b:a:0", bitrate,
		// Transcode to HLS-compatible fragmented MP4 files
		"-f", "hls", "-hls_segment_type", "fmp4",
		"-hls_init_time", "0", "-hls_time", strconv.Itoa(FileDuration+1),
		//"-hls_list_size", strconv.Itoa(FileCountLimit),
		"-hls_flags", "delete_segments+append_list+omit_endlist+round_durations+program_date_time",
		"-hls_playlist_type", "event",
		"-hls_fmp4_init_filename", fmt.Sprintf("%s-%s-init.mp4", basenameWithoutExt, bitrate),
		"-hls_segment_filename", outputFile,
		// Enable experimental flags for flac->fmp4
		"-strict", "experimental",
		// Create playlist file
		fmt.Sprintf("%s/playlist-%d.m3u8", MediaDir, index),
	)
	cmd.CombinedOutput()
}

func updateHLSPlaylist() {
	if HLSMasterPlaylist != nil {
		// Each transcode call writes/updates its own playlist file, while the master playlist is updated once here
		if len(HLSMasterPlaylist.Variants) != 2 {
			// These playlist names/variant params should match the ones in transcode()
			HLSMasterPlaylist.Append("playlist-0.m3u8", nil, m3u8.VariantParams{ProgramId: 1, Bandwidth: 192000, Codecs: "mp4a.40.2"})
			HLSMasterPlaylist.Append("playlist-1.m3u8", nil, m3u8.VariantParams{ProgramId: 1, Bandwidth: 1411000})
			os.WriteFile(fmt.Sprintf("%s/index.m3u8", MediaDir), HLSMasterPlaylist.Encode().Bytes(), 0644)
		}
		inputFile := AudioFilenames[len(AudioFilenames)-1]
		// Convert to 192k AAC segment per spec: https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices
		transcode(inputFile, "aac", "192k", 0)
		// Convert to 1411kbps FLAC segment for lossless: https://www.gearpatrol.com/tech/audio/a36585957/lossless-audio-explained/
		transcode(inputFile, "flac", "1411k", 1)
	}
}

func flush(frameBuffer []frame.Frame, sampleRate int) {
	if len(frameBuffer) <= 0 {
		return
	}
	encoder, err := openFLAC(sampleRate)
	if err != nil {
		log.Error(err, "Failed to create FLAC encoder")
		return
	}
	defer encoder.Close()
	for _, frame := range frameBuffer {
		if err := encoder.WriteFrame(&frame); err != nil {
			log.Error(err, "Failed to write FLAC frame")
			return
		}
	}
	updateHLSPlaylist()
}
