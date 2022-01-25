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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

// WebSocketManager is used to manage a websocket connection to the control plane
type WebSocketManager struct {
	Conn             *websocket.Conn
	Mu               sync.Mutex
	IsInitialized    bool
	APIOrigin        string
	Credentials      client.AgentCredentials
	ConfigChannel    chan client.AgentConfig
	HeartbeatChannel chan interface{}
	HeartbeatPath    string
}

// InitConnection initializes a new connection if there is no connection or returns an existing connection
func (wsm *WebSocketManager) InitConnection(wg *sync.WaitGroup, id string) error {
	if wsm.IsInitialized {
		return nil
	}

	// Parse url and format a ws(s) url
	u, _ := url.Parse(wsm.APIOrigin)
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	path := fmt.Sprintf("%s%s", u.Path, fmt.Sprintf(wsm.HeartbeatPath, id))
	wsURL := url.URL{Scheme: scheme, Host: u.Host, Path: path}

	// Initialize a websocket to the control plane
	wsm.Mu.Lock()
	h := http.Header{"Origin": []string{"http://jacktrip.local"}}
	h.Set("APISecret", wsm.Credentials.APISecret)
	h.Set("APIPrefix", wsm.Credentials.APIPrefix)
	c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), h)
	wsm.Conn = c
	wsm.Mu.Unlock()

	if err != nil {
		wsm.IsInitialized = false
		return err
	}

	wsm.IsInitialized = true
	log.Info("Websocket connected", "target", wsURL.String())

	return nil
}

// CloseConnection closes an initialized connection in a websocketmanager
func (wsm *WebSocketManager) CloseConnection() {
	wsm.Mu.Lock()
	wsm.Conn.Close()
	wsm.IsInitialized = false
	wsm.Mu.Unlock()
}

// Handlers to be used as a Goroutine

func (wsm *WebSocketManager) recvConfigHandler(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info("Starting recvConfigHandler")

	for {
		if !wsm.IsInitialized {
			// sleep while not connected to avoid inf loop
			time.Sleep(time.Second)
			continue
		}

		// read config message
		wsm.Conn.SetReadDeadline(time.Now().Add(time.Minute * 5)) // timeout after 5 minutes
		_, message, err := wsm.Conn.ReadMessage()
		if err != nil {
			log.Error(err, "[Websocket] Error reading message. Closing the connection.")
			wsm.CloseConnection()
			continue
		}

		var config client.AgentConfig
		if err := json.Unmarshal(message, &config); err != nil {
			log.Error(err, "Failed to unmarshal heartbeat response")
			continue
		}

		wsm.ConfigChannel <- config
	}
}

func (wsm *WebSocketManager) sendHeartbeatHandler(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info("Starting sendHeartbeatHandler")

	for {
		beat := <-wsm.HeartbeatChannel
		if wsm.IsInitialized {
			beatBytes, err := json.Marshal(beat)
			if err != nil {
				log.Error(err, "Failed to marshal heartbeat message")
				continue
			}

			err = wsm.Conn.WriteMessage(websocket.TextMessage, beatBytes)

			if err != nil {
				log.Error(err, "[Websocket] Failed to send a message. Closing the connection.")
				wsm.CloseConnection()
			} else {
				log.V(1).Info("Sent heartbeat message via websocket")
			}
		}
	}
}
