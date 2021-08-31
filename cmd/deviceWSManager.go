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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	statusPath = "/devices/%s/status"
)

// WebSocketManager is used to manage a websocket connection to the control plane
type WebSocketManager struct {
	Conn             *websocket.Conn
	Mu               sync.Mutex
	ReadMu           sync.Mutex
	WriteMu          sync.Mutex
	IsInitialized    bool
	ConfigChannel    chan client.AgentConfig
	PingStatsChannel chan client.PingStats
}

// InitConnection initializes a new connection if there is no connection or returns an existing connection
func (wsm *WebSocketManager) InitConnection(wg *sync.WaitGroup, apiOrigin string, macAddr string) error {
	if wsm.IsInitialized == true {
		return nil
	}

	// Parse url and format a ws(s) url
	u, _ := url.Parse(apiOrigin)
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	path := fmt.Sprintf("%s%s", u.Path, fmt.Sprintf(statusPath, macAddr))
	wsURL := url.URL{Scheme: scheme, Host: u.Host, Path: path}
	log.Info("Websocket connecting", "target", wsURL.String())

	// Initialize a websocket to the control plane
	wsm.Mu.Lock()
	h := http.Header{"Origin": []string{"http://jacktrip.local"}}
	c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), h)
	wsm.Conn = c
	wsm.Mu.Unlock()

	if err != nil {
		wsm.IsInitialized = false
		log.Error(err, "Websocket initialization error")
	} else {
		wg.Add(1)
		go wsm.runRecvConfigHandler(wg)
		wg.Add(1)
		go wsm.runSendPingStatsHandler(wg)

		wsm.IsInitialized = true
		log.Info("Websocket connected", "target", wsURL.String())
	}

	return err
}

// CloseConnection closes an initialized connection in a websocketmanager
func (wsm *WebSocketManager) CloseConnection() {
	wsm.Mu.Lock()
	wsm.Conn.Close()
	wsm.IsInitialized = false
	wsm.Mu.Unlock()
}

// Handlers to be used as a Goroutine

func (wsm *WebSocketManager) runRecvConfigHandler(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info("Starting runRecvConfigHandler")
	for {
		wsm.ReadMu.Lock()
		_, message, err := wsm.Conn.ReadMessage()
		wsm.ReadMu.Unlock()
		
		var config client.AgentConfig
		if err != nil {
			log.Error(err, "[Websocket] Error reading message. Closing the connection.")
			wsm.CloseConnection()
			return
		}

		if err := json.Unmarshal(message, &config); err != nil {
			log.Error(err, "Failed to unmarshal agent ping response")
			continue
		}

		wsm.ConfigChannel <- config
		time.Sleep(10 * time.Millisecond)
	}
}

func (wsm *WebSocketManager) runSendPingStatsHandler(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info("Starting runSendPingStatsHandler")
	for {
		select {
		case pingStats := <-wsm.PingStatsChannel:
			pingBytes, err := json.Marshal(pingStats)
			if err != nil {
				log.Error(err, "Failed to marshal ping stats")
				continue
			}

			wsm.WriteMu.Lock()
			err = wsm.Conn.WriteMessage(websocket.TextMessage, pingBytes)
			wsm.WriteMu.Unlock()

			if err != nil {
				log.Error(err, "[Websocket] Failed to send a message. Closing the connection.")
				wsm.CloseConnection()
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
