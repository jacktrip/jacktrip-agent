// Copyright 2020-2022 JackTrip Labs, Inc.
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

package client

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xthexder/go-jack"
)

const (
	// Default number of channels per client
	defaultChannels = 2
	// Regex pattern used to find zita ports
	zitaPortToken = `:(capture|playback)_`
	// Prefix tokens used to find hubserver ports
	hubserverInput  = "hubserver:send_"
	hubserverOutput = "hubserver:receive_"
	// Jamulus port names
	jamulusInputLeft   = "Jamulus:input left"
	jamulusInputRight  = "Jamulus:input right"
	jamulusOutputLeft  = "Jamulus:output left"
	jamulusOutputRight = "Jamulus:output right"
)

// AutoConnector manages JACK clients and keep tracks of clients
type AutoConnector struct {
	Name                string
	Channels            int
	JTRegexp            *regexp.Regexp
	JackClient          *jack.Client
	ClientLock          sync.Mutex
	KnownClients        map[string]int
	RegistrationChannel chan jack.PortId
}

// NewAutoConnector constructs a new instance of AutoConnector
func NewAutoConnector() *AutoConnector {
	return &AutoConnector{
		Name:                "autoconnector",
		Channels:            defaultChannels,
		JTRegexp:            regexp.MustCompile(zitaPortToken),
		KnownClients:        map[string]int{"Jamulus": 0},
		RegistrationChannel: make(chan jack.PortId, 200),
	}
}

// handlePortRegistration signals the notification channel when a new port is registered
// NOTE: We cannot modify ports in the callback thread so use a channel
func (ac *AutoConnector) handlePortRegistration(port jack.PortId, register bool) {
	if register {
		ac.RegistrationChannel <- port
	}
}

// getClientNum keeps track of new clients using a monotonically increasing number
func (ac *AutoConnector) getClientNum(name string) int {
	num, ok := ac.KnownClients[name]
	if !ok {
		num = len(ac.KnownClients)
		ac.KnownClients[name] = num
	}
	return num
}

// getServerChannel updates the client map and determines corresponding server channel numbers
func (ac *AutoConnector) getServerChannel(clientName string, clientChannel int) int {
	clientNum := ac.getClientNum(clientName)
	serverChannel := (clientNum * ac.Channels) + clientChannel
	return serverChannel
}

// getServerPortName finds the desired hubserver port name
func (ac *AutoConnector) getServerPortName(serverChannel int, isInput bool) string {
	var opt string

	// Use jamulus ports if available
	jil := ac.JackClient.GetPortByName(jamulusInputLeft)
	jir := ac.JackClient.GetPortByName(jamulusInputRight)
	jol := ac.JackClient.GetPortByName(jamulusOutputLeft)
	jor := ac.JackClient.GetPortByName(jamulusOutputRight)
	if jil != nil && jir != nil && jol != nil && jor != nil {
		opt = jamulusOutputLeft
		if serverChannel == 1 {
			if isInput {
				opt = jamulusInputLeft
			}
		}
		if serverChannel == 2 {
			opt = jamulusOutputRight
			if isInput {
				opt = jamulusInputRight
			}
		}
		return opt
	}

	// Use jacktrip "hubserver" ports otherwise
	opt = hubserverOutput
	if isInput {
		opt = hubserverInput
	}
	result := fmt.Sprintf("%s%d", opt, serverChannel)
	if ac.isValidPort(result) {
		return result
	}
	return ""
}

// isValidPort verifies a JACK port exists
func (ac *AutoConnector) isValidPort(name string) bool {
	if name == "" {
		return false
	}
	if p := ac.JackClient.GetPortByName(name); p != nil {
		return true
	}
	log.Info("Could not find JACK port", "name", name)
	return false
}

// isConnected checks if a JACK connection exists from src->dest
// NOTE: go-jack does not implement the "jack_port_connected_to"
func (ac *AutoConnector) isConnected(src, dest string) bool {
	p := ac.JackClient.GetPortByName(src)
	if p == nil {
		log.Error(errors.New("connection failed"), "JACK port no longer exists", "name", src)
		return true
	}
	for _, conn := range p.GetConnections() {
		if conn == dest {
			log.Info("JACK ports already connected", "src", src, "dest", dest)
			return true
		}
	}
	return false
}

// connectPorts establishes a directed connection between two JACK ports
func (ac *AutoConnector) connectPorts(src, dest string) {
	if ac.isConnected(src, dest) {
		return
	}
	code := ac.JackClient.Connect(src, dest)
	switch code {
	case 0:
		log.Info("Connected JACK ports", "src", src, "dest", dest)
	default:
		log.Error(jack.StrError(code), "Unexpected error connecting JACK ports", "src", src, "dest", dest)
	}
}

// connectSingleZitaPort establishes individual JackTrip/Jamulus<->zita audio connections
func (ac *AutoConnector) connectSingleZitaPort(port *jack.Port) {
	suffix := port.GetShortName()

	isInput := true
	if strings.HasPrefix(suffix, "playback_") {
		isInput = false
	}

	serverPortName := ac.getServerPortName(1, isInput)
	if strings.HasSuffix(port.GetName(), "_2") {
		serverPortName = ac.getServerPortName(2, isInput)
		if !ac.isValidPort(serverPortName) {
			serverPortName = ac.getServerPortName(1, isInput)
		}
	}

	if ac.isValidPort(serverPortName) {
		if isInput {
			ac.connectPorts(port.GetName(), serverPortName)
		} else {
			ac.connectPorts(serverPortName, port.GetName())
		}
	}
}

// connectAllZitaPorts establishes all JackTrip<->zita audio connections (used during initiation)
func (ac *AutoConnector) connectAllZitaPorts() {
	// Iterate over all output + input ports that match JackTrip pattern
	flags := []uint64{jack.PortIsOutput, jack.PortIsInput}
	for _, flag := range flags {
		ports := ac.JackClient.GetPorts(zitaPortToken, "", flag)
		for _, port := range ports {
			if strings.HasPrefix(port, "system:") {
				continue
			}
			jackPort := ac.JackClient.GetPortByName(port)
			if jackPort == nil {
				log.Error(errors.New("connection failed"), "JACK port no longer exists", "name", port)
			} else {
				ac.connectSingleZitaPort(jackPort)
			}
		}
	}
}

// onShutdown only runs upon unexpected connection error
func (ac *AutoConnector) onShutdown() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	ac.JackClient = nil
	// Wait for jackd to restart, then notify channel recipient to re-initialize client
	time.Sleep(5 * time.Second)
	ac.RegistrationChannel <- jack.PortId(0)
}

// InitJackClient creates a new JACK client
func InitJackClient(name string, prc jack.PortRegistrationCallback, sc jack.ShutdownCallback, pc jack.ProcessCallback, preActivationMethod func(client *jack.Client), close bool) (*jack.Client, error) {
	client, code := jack.ClientOpen(name, jack.NoStartServer)
	if client == nil || code != 0 {
		err := jack.StrError(code)
		log.Error(err, "Failed to create client")
		return nil, err
	}
	// Set port registration handler
	if prc != nil {
		if code := client.SetPortRegistrationCallback(prc); code != 0 {
			err := jack.StrError(code)
			log.Error(jack.StrError(code), "Failed to set port registration callback")
			return nil, err
		}
	}
	// Set process handler
	if pc != nil {
		if code := client.SetProcessCallback(pc); code != 0 {
			err := jack.StrError(code)
			log.Error(jack.StrError(code), "Failed to set process callback")
			return nil, err
		}
	}
	// Set shutdown handler
	if sc != nil {
		client.OnShutdown(sc)
	}
	// Call any special routine prior to (like establishing ports)
	if preActivationMethod != nil {
		preActivationMethod(client)
	}
	if code := client.Activate(); code != 0 {
		err := jack.StrError(code)
		log.Error(err, "Failed to activate client")
		return nil, err
	}
	// Automatically close client upon creation - used for connection checking
	if close {
		if code := client.Close(); code != 0 {
			err := jack.StrError(code)
			log.Error(err, "Failed to close client")
			return nil, err
		}
		return nil, nil
	}
	return client, nil
}

// Helper function to connect JACK ports in a thread-safe manner
func (ac *AutoConnector) connect(portID jack.PortId) error {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	if ac.JackClient == nil {
		err := WaitForJackd()
		if err != nil {
			return err
		}
		client, err := InitJackClient(ac.Name, ac.handlePortRegistration, ac.onShutdown, nil, nil, false)
		if err != nil {
			return err
		}
		ac.JackClient = client
		// Trigger a full-scan on initiation
		ac.connectAllZitaPorts()
	} else {
		port := ac.JackClient.GetPortById(portID)
		name := port.GetName()
		match := ac.JTRegexp.MatchString(name) && !strings.HasPrefix(name, "system:")
		if match {
			ac.connectSingleZitaPort(port)
		}
		if strings.HasPrefix(name, "Jamulus:") || strings.HasPrefix(name, "hubserver:") {
			ac.connectAllZitaPorts()
		}
	}
	return nil
}

// TeardownClient closes the currently active JACK client
func (ac *AutoConnector) TeardownClient() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	if ac.JackClient != nil {
		ac.JackClient.Close()
	}
	ac.JackClient = nil
	log.Info("Teardown of JACK client completed")
}

// SetupClient establishes a new client to watch for new JACK ports
func (ac *AutoConnector) SetupClient() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	err := WaitForJackd()
	if err != nil {
		panic(err)
	}
	client, err := InitJackClient(ac.Name, ac.handlePortRegistration, ac.onShutdown, nil, nil, false)
	if err != nil {
		panic(err)
	}
	ac.JackClient = client
	// Trigger a full-scan on initiation
	ac.connectAllZitaPorts()
	log.Info("Setup of JACK client completed", "name", ac.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (ac *AutoConnector) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping autoconnector")
			ac.TeardownClient()
			return
		case portID, ok := <-ac.RegistrationChannel:
			if !ok {
				log.Info("Registration channel is closed")
				return
			}
			err := RetryWithBackoff(func() error {
				return ac.connect(portID)
			})
			if err != nil {
				log.Error(err, "Failed to connect ports")
			}
		}
	}
}

// WaitForJackd is a jack_wait reimplementation
func WaitForJackd() error {
	err := RetryWithBackoff(func() error {
		_, err := InitJackClient("", nil, nil, nil, nil, true)
		return err
	})
	if err != nil {
		log.Error(err, "Unable to find JACK daemon")
		return err
	}
	log.Info("Found running JACK daemon")
	return nil
}
