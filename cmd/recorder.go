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
	"sync"
	"time"

	"github.com/xthexder/go-jack"
)

var AudioInPorts []*jack.Port

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

// processBuffer reads frames from the port's buffer
func processBuffer(nframes uint32) int {
	/* TODO: I'm getting 128-length arrays with floats...now what do I do
	for _, in := range AudioInPorts {
		samplesIn := in.GetBuffer(nframes)
		fmt.Println(samplesIn)
		fmt.Println(len(samplesIn))
	}
	*/
	return 0
}

// onShutdown only runs when upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.JackClient = nil
	AudioInPorts = nil
	// Wait for jackd to restart, then notify channel recipient to re-initialize client
	time.Sleep(5 * time.Second)
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
	log.Info("Teardown of JACK client completed")
}

// SetupClient establishes a new client to listen in on JACK ports
func (r *Recorder) SetupClient() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	err := waitForDaemon()
	if err != nil {
		panic(err)
	}
	portRegistrationFunc := func(client *jack.Client) {
		for i := 1; i <= 2; i++ {
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
