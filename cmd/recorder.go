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
	"sync"
	"time"

	"github.com/go-audio/wav"
	"github.com/xthexder/go-jack"
)

const (
	FileDuration   = 10
	FileCountLimit = 10
	NumChannels    = 2
	// Changing this will also involve changes in AudioSampleBuffer and in the process handler
	BitDepth = 16
	MaxBit   = math.MaxInt16
)

var (
	AudioInPorts      []*jack.Port
	AudioFilenames    []string
	AudioSampleBuffer []uint16
	JackSampleRate    int
	JackBufferSize    int
	SampleCounter     int
	fileHandler       *os.File
	wavLock           sync.Mutex
	wavOut            *wav.Encoder
)

// Recorder listens to audio and records it to disk
type Recorder struct {
	Name       string
	JackClient *jack.Client
	ClientLock sync.Mutex
	Shutdown   chan struct{}
}

// NewRecorder constructs a new instance of Recorder
func NewRecorder() *Recorder {
	return &Recorder{
		Name:     "recorder",
		Shutdown: make(chan struct{}),
	}
}

func openWavSafe() {
	wavLock.Lock()
	defer wavLock.Unlock()
	openWavUnsafe()
}

func openWavUnsafe() {
	// TODO: Make filename secret-like
	now := time.Now().Unix()
	filename := fmt.Sprintf("%s/test-%d.wav", MediaDir, now)
	fileHandler, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	// Keep track of files created and rotate files
	AudioFilenames = append(AudioFilenames, filename)
	if len(AudioFilenames) >= FileCountLimit {
		toRemove := AudioFilenames[0]
		AudioFilenames = AudioFilenames[1:]
		os.Remove(toRemove)
	}
	wavOut = wav.NewEncoder(fileHandler, JackSampleRate, BitDepth, NumChannels, 1)
}

func closeWavSafe() {
	wavLock.Lock()
	defer wavLock.Unlock()
	closeWavUnsafe()
}

func closeWavUnsafe() {
	if wavOut != nil {
		wavOut.Close()
		wavOut = nil
	}
	if fileHandler != nil {
		fileHandler.Close()
		fileHandler = nil
	}
}

func flush(sampleBuffer []uint16) {
	if len(sampleBuffer) > 0 {
		openWavSafe()
		for _, sample := range sampleBuffer {
			wavOut.WriteFrame(sample)
		}
		closeWavSafe()
	}
}

// processBuffer reads frames from the port's buffer
func processBuffer(nframes uint32) int {
	if len(AudioSampleBuffer) >= JackSampleRate*NumChannels*FileDuration {
		go flush(AudioSampleBuffer)
		AudioSampleBuffer = []uint16{}
	}
	size := JackBufferSize * NumChannels
	if size <= 0 {
		return 0
	}
	interleaved := make([]uint16, size)
	for i, port := range AudioInPorts {
		samples := port.GetBuffer(nframes)
		for j, sample := range samples {
			interleaved[j*NumChannels+i] = uint16(sample * MaxBit)
		}
	}
	AudioSampleBuffer = append(AudioSampleBuffer, interleaved...)
	return 0
}

// onShutdown only runs when upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.JackClient = nil
	AudioInPorts = nil
	closeWavSafe()
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
	closeWavSafe()
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
	log.Info("Setup of JACK client completed", "name", r.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (r *Recorder) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		_, ok := <-r.Shutdown
		if !ok {
			log.Info("Registration channel is closed")
			return
		}
	}
}
