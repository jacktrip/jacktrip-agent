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
	"strconv"
	"strings"
	"sync"

	"github.com/xthexder/go-jack"
)

const (
	// CHANNELS is the default number of channels per client
	CHANNELS = 2
	// EEXIST is an error code used to indicate existing connections
	EEXIST = 17
	// JT_* are pattern-matching tokens used to discover JACK ports
	JT_RECEIVE    = ":receive_"
	JT_SEND       = ":send_"
	JT_RECEIVE_RX = ".*:receive_.*"
	JT_SEND_RX    = ".*:send_.*"
	// SC_* are pattern-matching tokens used to find SuperCollider ports
	SC_IN  = "SuperCollider:in_"
	SC_OUT = "SuperCollider:out_"
	// SN_* are pattern-matching tokens used to find supernova ports
	SN_IN  = "supernova:input_"
	SN_OUT = "supernova:output_"
	// JAMULUS_* are pattern-matching tokens used to find Jamulus ports
	JAMULUS_INPUT_LEFT   = "Jamulus:input left"
	JAMULUS_INPUT_RIGHT  = "Jamulus:input right"
	JAMULUS_OUTPUT_LEFT  = "Jamulus:output left"
	JAMULUS_OUTPUT_RIGHT = "Jamulus:output right"
)

// registrationChan is a singleton channel used for notifications
var registrationChan chan jack.PortId = make(chan jack.PortId)

type AutoConnector struct {
	Name         string
	Channels     int
	JackClient   *jack.Client
	ClientLock   sync.Mutex
	KnownClients map[string]int
}

func NewAutoConnector() *AutoConnector {
	return &AutoConnector{
		Name:         "autoconnector",
		Channels:     CHANNELS,
		KnownClients: map[string]int{"Jamulus": 0},
	}
}

// handlePortRegistration signals the notification channel when a new port is registered
// NOTE: We cannot modify ports in the callback/notification thread so use the notification channel
func handlePortRegistration(port jack.PortId, register bool) {
	if register {
		registrationChan <- port
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

// getServerPortName finds the first-available port between SuperCollider and supernova
func (ac *AutoConnector) getServerPortName(serverChannel int, input bool) string {
	opts := []string{SC_OUT, SN_OUT}
	if input {
		opts = []string{SC_IN, SN_IN}
	}
	for _, opt := range opts {
		result := fmt.Sprintf("%s%d", opt, serverChannel)
		if ac.isValidPort(result) {
			return result
		}
	}
	return ""
}

// isValidPort verifies a JACK port exists
func (ac *AutoConnector) isValidPort(name string) bool {
	if p := ac.JackClient.GetPortByName(name); p != nil {
		return true
	}
	log.Info("Could not find JACK port", "name", name)
	return false
}

// connectPorts establishes a directed connection between two JACK ports
// NOTE: go-jack does not implement the "jack_port_connected_to" - handling this by ignoring EEXIST codes
func (ac *AutoConnector) connectPorts(src, dest string) {
	code := ac.JackClient.Connect(src, dest)
	switch code {
	case 0:
		log.Info(fmt.Sprintf("Connected output port %s to %s", src, dest))
	case EEXIST:
	default:
		log.Error(jack.StrError(code), "Unexpected error occurred connecting JACK ports")
	}
}

// connectJackTripSuperCollider establishes JackTrip<->SuperCollider audio connections
func (ac *AutoConnector) connectJackTripSuperCollider() {
	var clientName, serverPortName string
	var clientChannelNum, serverChannel int

	// Iterate over all output ports that match JackTrip receive pattern
	outPorts := ac.JackClient.GetPorts(JT_RECEIVE_RX, "", jack.PortIsOutput)
	for _, port := range outPorts {
		clientName, clientChannelNum = extractClientInfo(port, JT_RECEIVE)
		serverChannel = ac.getServerChannel(clientName, clientChannelNum)
		serverPortName = ac.getServerPortName(serverChannel, true)
		if ac.isValidPort(serverPortName) {
			ac.connectPorts(port, serverPortName)
		}
	}

	// Iterate over all input ports that match JackTrip send pattern
	inPorts := ac.JackClient.GetPorts(JT_SEND_RX, "", jack.PortIsInput)
	for _, port := range inPorts {
		clientName, clientChannelNum = extractClientInfo(port, JT_SEND)
		serverChannel = ac.getServerChannel(clientName, clientChannelNum)
		serverPortName = ac.getServerPortName(serverChannel, false)
		if ac.isValidPort(serverPortName) {
			ac.connectPorts(serverPortName, port)
		}
	}
}

// connectJamulusSuperCollider establishes Jamulus<->SuperCollider audio connections
func (ac *AutoConnector) connectJamulusSuperCollider() {
	// Return early if Jamulus ports are not active
	jil := ac.JackClient.GetPortByName(JAMULUS_INPUT_LEFT)
	jir := ac.JackClient.GetPortByName(JAMULUS_INPUT_RIGHT)
	jol := ac.JackClient.GetPortByName(JAMULUS_OUTPUT_LEFT)
	jor := ac.JackClient.GetPortByName(JAMULUS_OUTPUT_RIGHT)
	if jil == nil || jir == nil || jol == nil || jor == nil {
		return
	}

	clientNum := ac.getClientNum("Jamulus")
	leftChannelNum := (clientNum * ac.Channels) + 1
	rightChannelNum := (clientNum * ac.Channels) + 2
	var serverPortName string

	// Connect Jamulus input left
	serverPortName = ac.getServerPortName(leftChannelNum, false)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(serverPortName, JAMULUS_INPUT_LEFT)
	}
	// Connect Jamulus input right
	serverPortName = ac.getServerPortName(rightChannelNum, false)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(serverPortName, JAMULUS_INPUT_RIGHT)
	}
	// Connect Jamulus output left
	serverPortName = ac.getServerPortName(leftChannelNum, true)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(JAMULUS_OUTPUT_LEFT, serverPortName)
	}
	// Connect Jamulus output right
	serverPortName = ac.getServerPortName(rightChannelNum, true)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(JAMULUS_OUTPUT_RIGHT, serverPortName)
	}
}

func (ac *AutoConnector) TeardownClient() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	if ac.JackClient != nil {
		ac.JackClient.Close()
		ac.JackClient = nil
	}
}

func (ac *AutoConnector) SetupClient() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	client, code := jack.ClientOpen(ac.Name, jack.NoStartServer)
	if code != 0 {
		log.Error(jack.StrError(code), "Failed to create client")
	}
	if code := client.SetPortRegistrationCallback(handlePortRegistration); code != 0 {
		log.Error(jack.StrError(code), "Failed to set port registration callback")
	}
	if code := client.Activate(); code != 0 {
		log.Error(jack.StrError(code), "Failed to activate client")
	}
	ac.JackClient = client
}

// Helper function to connect JACK ports in a thread-safe manner
func (ac *AutoConnector) connect() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	if ac.JackClient != nil {
		ac.connectJackTripSuperCollider()
		ac.connectJamulusSuperCollider()
	}
}

func (ac *AutoConnector) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		// The data sent on this channel is only used for notification purposes
		_, ok := <-registrationChan
		if !ok {
			log.Info("Registration channel is closed")
			return
		}
		ac.connect()
	}
}

// Extract name and channel number from a given JACK port
func extractClientInfo(port, sep string) (string, int) {
	data := strings.SplitN(port, sep, 2)
	name := data[0]
	channel, _ := strconv.Atoi(data[1])
	return name, channel
}
