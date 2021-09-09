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
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// ServerStatusPath is a WSS API route used for bi-directional updates for a given studio
	ServerStatusPath = "/servers/%s/status"

	// MaxClientsPerProcessor is the number of JackTrip clients to support per logical processor
	MaxClientsPerProcessor = 6 // add 1 (20%) for Jamulus bridge, etc

	// SCSynthServiceName is the name of the systemd service for the SuperCollider scsynth server
	SCSynthServiceName = "scsynth.service"

	// SupernovaServiceName is the name of the systemd service for the SuperCollider supernova server
	SupernovaServiceName = "supernova.service"

	// SCLangServiceName is the name of the systemd service for the SuperCollider language runtime
	SCLangServiceName = "sclang.service"

	// JackAutoconnectServiceName is the name of the systemd service for connecting jack clients
	JackAutoconnectServiceName = "jack-autoconnect.service"

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

	// EC2InstanceIDURL is url using EC2 metadata service that returns the instance-id
	// See https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html
	EC2InstanceIDURL = "http://169.254.169.254/latest/meta-data/instance-id"

	// GCloudInstanceIDURL is url using Google Cloud metadata service that returns the instance name
	// See https://cloud.google.com/compute/docs/storing-retrieving-metadata
	GCloudInstanceIDURL = "http://metadata.google.internal/computeMetadata/v1/instance/name"

	// AzureInstanceIDURL is url using Azure metadata service that returns the instance name
	// See https://docs.microsoft.com/en-us/azure/virtual-machines/linux/instance-metadata-service?tabs=linux
	AzureInstanceIDURL = "http://169.254.169.254/metadata/instance/compute/name?api-version=2017-08-01&format=text"
)

var lastConfig client.AgentConfig

// runOnServer is used to run jacktrip-agent on an audio cloud server
func runOnServer(apiOrigin string) {
	// setup wait group for multiple routines
	var wg sync.WaitGroup

	log.Info("Running jacktrip-agent in server mode")

	// start HTTP server to respond to pings
	router := mux.NewRouter()
	router.HandleFunc("/ping", handlePingRequest).Methods("GET")
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		OptionsGetOnly(w, r)
	})).Methods("OPTIONS")
	wg.Add(1)
	go runHTTPServer(&wg, router, fmt.Sprintf("0.0.0.0:%s", HTTPServerPort))

	// get cloud id
	cloudID := getCloudID()

	// TODO: get credentials
	credentials := client.AgentCredentials{}

	// start ping server to send pings and update agent config
	ping := client.AgentPing{
		AgentCredentials: credentials,
		CloudID:          cloudID,
		Version:          getPatchVersion(),
	}
	wg.Add(1)
	go runServerPinger(&wg, ping, apiOrigin)

	// wait for everything to complete
	wg.Wait()
}

// runServerPinger sends pings to service and manages config updates
func runServerPinger(wg *sync.WaitGroup, ping client.AgentPing, apiOrigin string) {
	defer wg.Done()

	log.Info("Starting agent ping server")

	for {
		config, err := sendPing(ping, apiOrigin)
		if err != nil {
			panic(err)
		}

		// check if config has changed
		if config != lastConfig {
			log.Info("Config updated", "value", config)
			handleServerUpdate(config)
		}

		// sleep in between pings
		time.Sleep(AgentPingInterval * time.Second)
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
		restartAllServices(config, true)
	}

	lastConfig = config
}

// getCloudID retrieves cloud instance id, via metadata service
func getCloudID() string {
	// send request to get instance-id from EC2 metadata
	r, err := http.Get(EC2InstanceIDURL)
	if err != nil || r.StatusCode != http.StatusOK {
		// try again using Google Cloud metadata
		client := &http.Client{}
		req, _ := http.NewRequest("GET", GCloudInstanceIDURL, nil)
		req.Header.Set("Metadata-Flavor", "Google")
		r, err = client.Do(req)
		if err != nil || r.StatusCode != http.StatusOK {
			// try again using Azure metadata
			req, _ = http.NewRequest("GET", AzureInstanceIDURL, nil)
			req.Header.Set("Metadata", "true")
			r, err = client.Do(req)
			if err != nil || r.StatusCode != http.StatusOK {
				log.Error(err, "Failed to retrieve instance-id from metadata service")
				panic(err)
			}
		}
	}
	defer r.Body.Close()

	// decode config from response
	bodyBytes, err := ioutil.ReadAll(r.Body)
	cloudID := string(bodyBytes)
	if err != nil || cloudID == "" {
		err = errors.New("failed to retrieve instance-id from metadata service")
		log.Error(err, "Empty response")
		panic(err)
	}

	log.Info("Retrieved instance-id", "id", cloudID)

	return cloudID
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
