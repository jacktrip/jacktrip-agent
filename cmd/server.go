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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// ServerHeartbeatPath is a WSS API route used for bi-directional updates for a given studio
	ServerHeartbeatPath = "/servers/%s/heartbeat"

	// MaxClientsPerProcessor is the number of JackTrip clients to support per logical processor
	MaxClientsPerProcessor = 6 // add 1 (20%) for Jamulus bridge, etc

	// SCSynthServiceName is the name of the systemd service for the SuperCollider scsynth server
	SCSynthServiceName = "scsynth.service"

	// SupernovaServiceName is the name of the systemd service for the SuperCollider supernova server
	SupernovaServiceName = "supernova.service"

	// SCLangServiceName is the name of the systemd service for the SuperCollider language runtime
	SCLangServiceName = "sclang.service"

	// JamulusServerServiceName is the name of the systemd service for the Jamulus server
	JamulusServerServiceName = "jamulus-server.service"

	// JamulusBridgeServiceName is the name of the systemd service for the Jamulus -> JackTrip  bridge
	JamulusBridgeServiceName = "jamulus-bridge.service"

	// PathToSCLangConfig is the path to SuperCollider sclang service config file
	PathToSCLangConfig = "/tmp/default/sclang"

	// PathToSuperColliderConfig is the path to SuperCollider service config file
	PathToSuperColliderConfig = "/tmp/default/supercollider"

	// PathToSuperColliderStartupFile is the path to SuperCollider startup file
	PathToSuperColliderStartupFile = "/tmp/jacktrip.scd"

	// JackServerConfigTemplate is the template used to generate /tmp/default/jack file on audio servers
	JackServerConfigTemplate = "JACK_OPTS=-d dummy --rate %d --period %d\n"

	// JackTripServerConfigTemplate is the template used to generate /tmp/default/jacktrip file on audio servers
	JackTripServerConfigTemplate = "JACKTRIP_OPTS=-S -t -z --bindport %d --nojackportsconnect --broadcast 1024 %s\n"

	// SCLangConfigTemplate is the template used to generate /tmp/default/sclang file on audio servers
	SCLangConfigTemplate = "SCLANG_OPTS=%s %s\n"

	// SuperColliderConfigTemplate is the template used to generate /tmp/default/supercollider file on audio servers
	SuperColliderConfigTemplate = "SC_OPTS=-i %d -o %d -a %d -m %d -z %d -n 4096 -d 2048 -w 2048\n"
)

var lastConfig client.AgentConfig
var ac *AutoConnector

// runOnServer is used to run jacktrip-agent on an audio cloud server
func runOnServer(apiOrigin string) {
	// get server identifier
	serverID := os.Getenv("JACKTRIP_SERVER_ID")
	if serverID == "" {
		err := errors.New("Empty server identifier")
		log.Error(err, "unique identifier is required for server mode")
		os.Exit(1)
	}

	// TODO: get server credentials
	credentials := client.AgentCredentials{}

	// CloudID may be empty if not on a managed cloud server
	beat := client.ServerHeartbeat{CloudID: os.Getenv("JACKTRIP_CLOUD_ID")}

	log.Info("Running jacktrip-agent in server mode")

	// setup wait group for multiple routines
	var wg sync.WaitGroup

	// start HTTP server to respond to pings
	router := mux.NewRouter()
	router.HandleFunc("/ping", handlePingRequest).Methods("GET")
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		OptionsGetOnly(w, r)
	})).Methods("OPTIONS")
	wg.Add(1)
	go runHTTPServer(&wg, router, fmt.Sprintf("0.0.0.0:%s", HTTPServerPort))

	// start WebSocketManager
	wsm := WebSocketManager{
		ConfigChannel:    make(chan client.AgentConfig, 100),
		HeartbeatChannel: make(chan interface{}, 100),
		APIOrigin:        apiOrigin,
		Credentials:      credentials,
		HeartbeatPath:    ServerHeartbeatPath,
	}
	wg.Add(1)
	go wsm.recvConfigHandler(&wg)
	wg.Add(1)
	go wsm.sendHeartbeatHandler(&wg)

	// start ping server to send pings and update agent config
	wg.Add(1)
	go sendServerHeartbeats(&wg, &beat, &wsm, serverID)

	// Start a config handler to update config changes
	wg.Add(1)
	go serverConfigUpdateHandler(&wg, &wsm)

	// Start JACK autoconnector
	ac = NewAutoConnector()
	wg.Add(1)
	go ac.Run(&wg)

	// wait for everything to complete
	wg.Wait()
}

// serverConfigUpdateHandler receives and processes server config updates
func serverConfigUpdateHandler(wg *sync.WaitGroup, wsm *WebSocketManager) {
	defer wg.Done()
	log.Info("Starting serverConfigUpdateHandler")
	for {
		newServerConfig, ok := <-wsm.ConfigChannel
		if !ok {
			log.Info("Config channel is closed")
			return
		}
		if newServerConfig != lastConfig {
			log.Info("Config updated", "value", newServerConfig)
			handleServerUpdate(newServerConfig)
		}
	}
}

// sendServerHeartbeats sends heartbeat messages to api server and manages config updates
func sendServerHeartbeats(wg *sync.WaitGroup, beat *client.ServerHeartbeat, wsm *WebSocketManager, serverID string) {
	defer wg.Done()

	log.Info("Sending server heartbeats")

	for {
		err := wsm.InitConnection(wg, serverID)
		if err != nil {
			log.Error(err, "Failed to initiate websocket conncetion")
			panic(err)
		}

		wsm.HeartbeatChannel <- *beat

		// sleep in between pings
		time.Sleep(HeartbeatInterval * time.Second)
	}
}

// handleServerUpdate handles updates to server configuratiosn
func handleServerUpdate(config client.AgentConfig) {

	// check if supercollider code was the only change
	lastConfig.MixBranch = config.MixBranch
	lastConfig.MixCode = config.MixCode
	if config == lastConfig {

		// only the supercollider code needs updated
		// update configs and restart sclang service to minimize disruption
		updateSuperColliderConfigs(config)
		err := restartService(SCLangServiceName)
		if err != nil {
			log.Error(err, "Unable to restart service", "name", SCLangServiceName)
		}

	} else {
		// more changes required -> reset everything

		// update managed config files
		updateServiceConfigs(config, "", true)

		// shutdown or restart managed services
		ac.TeardownClient()
		restartAllServices(config, true)
		// jack client will error when the server is only using Jamulus
		if config.Type != client.Jamulus {
			// this sleep was necessary otherwise ports aren't found?
			ac.SetupClient()
		}
	}

	lastConfig = config
}

// updateSuperColliderConfigs is used to update SuperCollider config files on managed audio servers
func updateSuperColliderConfigs(config client.AgentConfig) {
	// write SuperCollider (server) config file
	maxClients := runtime.NumCPU() * MaxClientsPerProcessor
	numInputChannels := maxClients * 2
	numOutputChannels := maxClients * 2
	audioBusses := (numInputChannels + numOutputChannels) * 2

	// bump memory for larger systems
	scMemorySize := 65536
	if maxClients > 50 {
		scMemorySize = 262144
	}

	// lower bufsize if jack is lower
	scBufSize := 64
	if config.Period < 64 {
		scBufSize = config.Period
	}

	// create service config using template
	scConfig := fmt.Sprintf(SuperColliderConfigTemplate,
		numInputChannels, numOutputChannels, audioBusses,
		scMemorySize, scBufSize)

	// write SuperCollider (supercollider) service config file
	err := ioutil.WriteFile(PathToSuperColliderConfig, []byte(scConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save SuperCollider config", "path", PathToSuperColliderConfig)
	}

	// write SuperCollider (sclang) service config file
	sclangConfig := fmt.Sprintf(SCLangConfigTemplate, config.MixBranch, PathToSuperColliderStartupFile)
	err = ioutil.WriteFile(PathToSCLangConfig, []byte(sclangConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save sclang config", "path", PathToSCLangConfig)
	}

	// write SuperCollider startup file
	scStartup := fmt.Sprintf(`~maxClients = %d;
~inputChannelsPerClient = 2;
~outputChannelsPerClient = 2;
%s`, maxClients, config.MixCode)
	err = ioutil.WriteFile(PathToSuperColliderStartupFile, []byte(scStartup), 0644)
	if err != nil {
		log.Error(err, "Failed to save SuperCollider startup file", "path", PathToSuperColliderStartupFile)
	}
}
