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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
	"github.com/xthexder/go-jack"
)

const (
	// Default number of channels per client
	defaultChannels = 2
	// Regex pattern used to find JackTrip ports
	jacktripPortToken = `:(send|receive)_`
	// Prefix tokens used to find SuperCollider ports
	supercolliderInput  = "SuperCollider:in_"
	supercolliderOutput = "SuperCollider:out_"
	// Prefix tokens used to find supernova ports
	supernovaInput  = "supernova:input_"
	supernovaOutput = "supernova:output_"
	// Jamulus port names
	jamulusInputLeft   = "Jamulus:input left"
	jamulusInputRight  = "Jamulus:input right"
	jamulusOutputLeft  = "Jamulus:output left"
	jamulusOutputRight = "Jamulus:output right"
)

// AutoConnector manages JACK clients and keep tracks of clients
type AutoConnector struct {
	Name             string
	Channels         int
	JTRegexp         *regexp.Regexp
	JackClient       *jack.Client
	ClientLock       sync.Mutex
	KnownClients     map[string]int
	RegistrationChan chan jack.PortId
}

// NewAutoConnector constructs a new instance of AutoConnector
func NewAutoConnector() *AutoConnector {
	return &AutoConnector{
		Name:             "autoconnector",
		Channels:         defaultChannels,
		JTRegexp:         regexp.MustCompile(jacktripPortToken),
		KnownClients:     map[string]int{"Jamulus": 0},
		RegistrationChan: make(chan jack.PortId, 200),
	}
}

// handlePortRegistration signals the notification channel when a new port is registered
// NOTE: We cannot modify ports in the callback thread so use a channel
func (ac *AutoConnector) handlePortRegistration(port jack.PortId, register bool) {
	if register {
		ac.RegistrationChan <- port
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
func (ac *AutoConnector) getServerPortName(serverChannel int, isInput bool) string {
	opts := []string{supercolliderOutput, supernovaOutput}
	if isInput {
		opts = []string{supercolliderInput, supernovaInput}
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
	for _, conn := range p.GetConnections() {
		if conn == dest {
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
		log.Error(jack.StrError(code), "Unexpected error connecting JACK ports")
	}
}

// connectSingleJackTripSCPort establishes individual JackTrip<->SuperCollider audio connections
func (ac *AutoConnector) connectSingleJackTripSCPort(port *jack.Port) {
	clientName := port.GetClientName()
	suffix := port.GetShortName()
	data := strings.SplitN(suffix, "_", 2)
	clientChannelNum, _ := strconv.Atoi(data[len(data)-1])
	isInput := true
	if strings.HasPrefix(suffix, "send_") {
		isInput = false
	}
	serverChannel := ac.getServerChannel(clientName, clientChannelNum)
	serverPortName := ac.getServerPortName(serverChannel, isInput)
	if ac.isValidPort(serverPortName) {
		if isInput {
			ac.connectPorts(port.GetName(), serverPortName)
		} else {
			ac.connectPorts(serverPortName, port.GetName())
		}
	}
}

// connectAllJackTripSCPorts establishes all JackTrip<->SuperCollider audio connections (used during initiation)
func (ac *AutoConnector) connectAllJackTripSCPorts() {
	// Iterate over all output + input ports that match JackTrip pattern
	flags := []uint64{jack.PortIsOutput, jack.PortIsInput}
	for _, flag := range flags {
		ports := ac.JackClient.GetPorts(jacktripPortToken, "", flag)
		for _, port := range ports {
			jackPort := ac.JackClient.GetPortByName(port)
			ac.connectSingleJackTripSCPort(jackPort)
		}
	}
}

// connectJamulusSuperCollider establishes all Jamulus<->SuperCollider audio connections (used during initiation)
func (ac *AutoConnector) connectJamulusSuperCollider() {
	// Return early if Jamulus ports are not active
	jil := ac.JackClient.GetPortByName(jamulusInputLeft)
	jir := ac.JackClient.GetPortByName(jamulusInputRight)
	jol := ac.JackClient.GetPortByName(jamulusOutputLeft)
	jor := ac.JackClient.GetPortByName(jamulusOutputRight)
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
		ac.connectPorts(serverPortName, jamulusInputLeft)
	}
	// Connect Jamulus input right
	serverPortName = ac.getServerPortName(rightChannelNum, false)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(serverPortName, jamulusInputRight)
	}
	// Connect Jamulus output left
	serverPortName = ac.getServerPortName(leftChannelNum, true)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(jamulusOutputLeft, serverPortName)
	}
	// Connect Jamulus output right
	serverPortName = ac.getServerPortName(rightChannelNum, true)
	if ac.isValidPort(serverPortName) {
		ac.connectPorts(jamulusOutputRight, serverPortName)
	}
}

// onShutdown only runs when upon unexpected connection error
func (ac *AutoConnector) onShutdown() {
	ac.ClientLock.Lock()
	defer ac.ClientLock.Unlock()
	ac.JackClient = nil
	// Wait for jackd to restart, then notify channel recipient to re-initialize client
	time.Sleep(5 * time.Second)
	ac.RegistrationChan <- jack.PortId(0)
}

func initClient(name string, prc jack.PortRegistrationCallback, sc jack.ShutdownCallback, pc jack.ProcessCallback, preActivationMethod func(client *jack.Client), close bool) (*jack.Client, error) {
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
		err := waitForDaemon()
		if err != nil {
			return err
		}
		client, err := initClient(ac.Name, ac.handlePortRegistration, ac.onShutdown, nil, nil, false)
		if err != nil {
			return err
		}
		ac.JackClient = client
		// Trigger a full-scan on initiation
		ac.connectAllJackTripSCPorts()
		ac.connectJamulusSuperCollider()
	} else {
		port := ac.JackClient.GetPortById(portID)
		match := ac.JTRegexp.MatchString(port.GetName())
		if match {
			ac.connectSingleJackTripSCPort(port)
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
	err := waitForDaemon()
	if err != nil {
		panic(err)
	}
	client, err := initClient(ac.Name, ac.handlePortRegistration, ac.onShutdown, nil, nil, false)
	if err != nil {
		panic(err)
	}
	ac.JackClient = client
	// Trigger a full-scan on initiation
	ac.connectAllJackTripSCPorts()
	ac.connectJamulusSuperCollider()
	log.Info("Setup of JACK client completed", "name", ac.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (ac *AutoConnector) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		portID, ok := <-ac.RegistrationChan
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

// CollectMetrics collects JACK + JackTrip metrics
// TODO: Jamulus
func (ac *AutoConnector) CollectMetrics() []client.ServerMetric {
	metrics := []client.ServerMetric{{Name: "virtual_studio_jackd_up", Value: 0}}
	jackClient, err := initClient("", nil, nil, nil, nil, false)
	if err != nil {
		return metrics
	}
	defer jackClient.Close()

	// Update "virtual_studio_up" now that jackd is running
	metrics[0].Value = 1

	allPorts := client.ServerMetric{Name: "virtual_studio_jack_connections_total", Value: 0}
	systemPorts := client.ServerMetric{Name: "virtual_studio_jack_connections_system", Value: 0}
	scPorts := client.ServerMetric{Name: "virtual_studio_jack_connections_supercollider", Value: 0}
	jamulusPorts := client.ServerMetric{Name: "virtual_studio_jack_connections_jamulus", Value: 0}
	clientPorts := client.ServerMetric{Name: "virtual_studio_jack_connections_clients_total", Value: 0}

	ports := jackClient.GetPorts(".*", "", 0)
	for _, port := range ports {
		allPorts.Value++
		if strings.HasPrefix(port, "system") {
			systemPorts.Value++
			continue
		}
		if strings.HasPrefix(port, "SuperCollider") || strings.HasPrefix(port, "supernova") {
			scPorts.Value++
			continue
		}
		if strings.HasPrefix(port, "Jamulus") {
			jamulusPorts.Value++
			continue
		}
		// Update known clients map
		clientPorts.Value++
		jackPort := jackClient.GetPortByName(port)
		ac.getClientNum(jackPort.GetClientName())
	}
	metrics = append(metrics, allPorts)
	metrics = append(metrics, systemPorts)
	metrics = append(metrics, scPorts)
	metrics = append(metrics, jamulusPorts)
	metrics = append(metrics, clientPorts)

	// Count the number of unique clients, removing Jamulus + recorder
	uniqueClients := client.ServerMetric{Name: "virtual_studio_jack_clients_unique", Value: float64(len(ac.KnownClients) - 2)}
	metrics = append(metrics, uniqueClients)
	for key := range ac.KnownClients {
		if key == "Jamulus" || key == "recorder" {
			continue
		}
		new := client.ServerMetric{Name: "virtual_studio_jack_clients", ClientName: key, Value: 1}
		metrics = append(metrics, new)
	}
	return metrics
}

// jack_wait reimplementation
func waitForDaemon() error {
	err := RetryWithBackoff(func() error {
		_, err := initClient("", nil, nil, nil, nil, true)
		return err
	})
	if err != nil {
		log.Error(err, "Unable to find JACK daemon")
		return err
	}
	log.Info("Found running JACK daemon")
	return nil
}