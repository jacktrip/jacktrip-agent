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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// AgentPingURL is the URL used to POST HTTP heartbeats
	AgentPingURL = "/agents/ping"

	// HeartbeatInterval is an interval between heartbeats
	HeartbeatInterval = 5
)

// sendHTTPHeartbeat sends HTTP heartbeat to api and receives latest config
func sendHTTPHeartbeat(beat interface{}, credentials client.AgentCredentials, apiOrigin string) (client.DeviceAgentConfig, error) {
	var config client.DeviceAgentConfig

	// update and encode heartbeat content
	beatBytes, err := json.Marshal(beat)
	if err != nil {
		log.Error(err, "Failed to marshal agent heartbeat request")
		return config, err
	}

	// send heartbeat request
	client := &http.Client{}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s%s", apiOrigin, AgentPingURL), bytes.NewReader(beatBytes))
	req.Header.Set("APIPrefix", credentials.APIPrefix)
	req.Header.Set("APISecret", credentials.APISecret)
	r, err := client.Do(req)
	if err != nil {
		log.Error(err, "Failed to send agent heartbeat request")
		return config, err
	}
	defer r.Body.Close()

	// check response status
	if r.StatusCode != http.StatusOK {
		return config, fmt.Errorf("bad response from agent heartbeat: Status=%d", r.StatusCode)
	}

	// decode config from response
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&config); err != nil {
		log.Error(err, "Failed to unmarshal agent heartbeat response")
		return config, err
	}

	log.V(1).Info("Sent heartbeat message via HTTP")

	return config, nil
}
