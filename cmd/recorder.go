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

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/xthexder/go-jack"
)

const (
	// FileDuration is the duration (in seconds) of each audio segment file
	FileDuration = 10
	// FileCountLimit is the maximum number of audio segment files kept on disk
	FileCountLimit = 5
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

	HLSPlaylistHash string
	hlsIndex        int
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

// onShutdown only runs upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.JackClient, r.RecorderPorts, r.JackSampleRate, r.JackBufferSize = nil, nil, 0, 0
	for _, trash := range AudioFilenames {
		rotateStaleFile(trash)
	}
	AudioFilenames, FrameBuffer = nil, nil
	hlsIndex = 0
	HLSPlaylistHash = ""
	os.Remove(filepath.Join(MediaDir, HLSIndex))
}

// TeardownClient closes the currently active JACK client
func (r *Recorder) TeardownClient() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	if r.JackClient != nil {
		r.JackClient.Close()
	}
	r.JackClient, r.RecorderPorts, r.JackSampleRate, r.JackBufferSize = nil, nil, 0, 0
	for _, trash := range AudioFilenames {
		rotateStaleFile(trash)
	}
	AudioFilenames, FrameBuffer = nil, nil
	hlsIndex = 0
	HLSPlaylistHash = ""
	os.Remove(filepath.Join(MediaDir, HLSIndex))
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
	// TODO: Should this be done here?
	hlsIndex = 0
	HLSPlaylistHash = GenerateRandomString(8)
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
	// Get current sample rate + buffer size in a thread-safe manner in case the JACK client fails
	r.ClientLock.Lock()
	sampleRate, bufferSize := r.JackSampleRate, r.JackBufferSize
	r.ClientLock.Unlock()
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
		rotateStaleFile(AudioFilenames[0])
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

// rotateStaleFile deletes all files that pattern-match the input filename (minus extension)
func rotateStaleFile(filename string) {
	prefix := pathutil.TrimExt(filename)
	files, _ := filepath.Glob(prefix + "*")
	for _, f := range files {
		os.Remove(f)
	}
}

func updateHLSPlaylist() {
	if HLSPlaylistHash != "" {
		inputFile := AudioFilenames[len(AudioFilenames)-1]
		basename := filepath.Base(inputFile)
		basenameWithoutExt := strings.TrimSuffix(basename, filepath.Ext(basename))
		/*
			duplicateFile := fmt.Sprintf("%s-copy.flac", strings.TrimSuffix(inputFile, filepath.Ext(inputFile)))
			basename := filepath.Base(inputFile)
			basenameWithoutExt := strings.TrimSuffix(basename, filepath.Ext(basename))
			flacVerification := exec.Command(
				"ffmpeg", "-i", inputFile,
				"-c:a", "flac", duplicateFile,
			)
			out, _ := flacVerification.CombinedOutput()
			fmt.Println(string(out))
		*/
		// Execute ffmpeg - all options described here: https://ffmpeg.org/ffmpeg-formats.html
		cmd := exec.Command(
			// Call ffmpeg on the most-recently created FLAC file
			"ffmpeg", "-i", inputFile,
			// Convert to 320kbps bitrate AAC sample
			"-map", "0:a", "-c:a:0", "aac", "-b:a:0", "320k",
			//"-map", "0:a", "-c:a:1", "aac", "-b:a:1", "160k",
			//"-map", "0:a", "-c:a:2", "aac", "-b:a:2", "96k",
			// Transcode to HLS-compatible fragmented MP4 files
			"-f", "hls", "-hls_segment_type", "fmp4", "-hls_init_time", "0",
			"-hls_playlist_type", "event", "-hls_flags", "delete_segments+append_list+omit_endlist+round_durations",
			"-hls_fmp4_init_filename", basenameWithoutExt+"-init.mp4",
			//"-hls_fmp4_init_filename", basenameWithoutExt+"-%v-init.mp4",
			"-hls_segment_filename", filepath.Join(MediaDir, basenameWithoutExt+"-%v-%03d.m4s"),
			"-hls_time", strconv.Itoa(FileDuration+1),
			// Enable experimental flags for flac->fmp4
			"-strict", "experimental",
			// Create master playlist file
			"-master_pl_name", HLSIndex,
			// Output each bitrate into a unique stream
			"-var_stream_map", "a:0", filepath.Join(MediaDir, "playlist_"+HLSPlaylistHash+"_%v.m3u8"),
			//"-var_stream_map", "a:0 a:1 a:2", filepath.Join(MediaDir, "playlist_%v.m3u8"),
		)
		fmt.Println(cmd.String())
		out, _ := cmd.CombinedOutput()
		fmt.Println(string(out))
		//if err != nil {
		//	log.Error(err, "Failed ffmpeg transcoding")
		//}
		// TODO: This library just makes exec.Run calls to ffmpeg...maybe we don't need it
		/*
			cmd := ffmpeg.Input(inputFile).Output(
				filepath.Join(MediaDir, HLSIndex),
				ffmpeg.KwArgs{
					"map":                    "0:a",
					"c:a:0":                  "aac",
					"b:a:0":                  "320k",
					"f":                      "hls",
					"hls_segment_type":       "fmp4",
					"strftime":               "1",
					"hls_segment_filename":   filepath.Join(MediaDir, basenameWithoutExt+"-%s.m4s"),
					"hls_fmp4_init_filename": basenameWithoutExt + "-init.mp4",
					"start_number":           strconv.Itoa(hlsIndex),
					"hls_init_time":          "0",
					"hls_time":               strconv.Itoa(FileDuration + 1),
					"hls_list_size":          "2",
					"hls_delete_threshold":   "1",
					"hls_flags":              "delete_segments+append_list+omit_endlist",
					"strict":                 "experimental",
				},
			)
			if err := cmd.Run(); err != nil {
				log.Error(err, "Failed ffmpeg transcoding")
			}
		*/
		hlsIndex += 1
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
