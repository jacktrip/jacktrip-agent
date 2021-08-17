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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// AgentPingURL is the URL used to POST agent pings
	AgentPingURL = "/agents/ping"
)

// AgentPingInterval is number of seconds in between agent ping requests
var AgentPingInterval = 5 * time.Second

var lastConfig client.AgentConfig

// sendPing sends ping to service to retrieve config
func sendPing(ping client.AgentPing, apiOrigin string) (client.AgentConfig, error) {
	var config client.AgentConfig

	// update and encode ping content
	pingBytes, err := json.Marshal(ping)
	if err != nil {
		log.Error(err, "Failed to marshal agent ping request")
		return config, err
	}

	// send ping request
	r, err := http.Post(fmt.Sprintf("%s%s", apiOrigin, AgentPingURL), "application/json", bytes.NewReader(pingBytes))
	if err != nil {
		log.Error(err, "Failed to send agent ping request")
		return config, err
	}
	defer r.Body.Close()

	// check response status
	if r.StatusCode != http.StatusOK {
		log.Info("Bad response from agent ping", "status", r.StatusCode)
		return config, err
	}

	// decode config from response
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&config); err != nil {
		log.Error(err, "Failed to unmarshal agent ping response")
		return config, err
	}

	return config, nil
}
