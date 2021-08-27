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

type webSocketConnector struct {
	Conn          *websocket.Conn
	Mu            sync.Mutex
	APIOrigin     string
	IsInitialized bool
}

// Connector is used as a singleton object of webSocketConnector
var Connector webSocketConnector

// ConfigChannel holds config objects that came through a websocket connection and is consumed by a handler in device.go
var ConfigChannel = make(chan client.AgentConfig, 100)

// ReceivePingChannel is used to control ReceivePingHandler Goroutine
var ReceivePingChannel chan bool

// InitWSConnection initializes a websocket connection to the API server and manages a connection
func InitWSConnection(apiOrigin string, ping client.AgentPing) error {
	var err error
	if Connector.IsInitialized == true {
		return err
	}

	u, _ := url.Parse(apiOrigin)
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := url.URL{Scheme: scheme, Host: u.Host, Path: fmt.Sprintf("%s%s", u.Path, fmt.Sprintf("/devices/%s/status", ping.MAC))}
	log.Info(fmt.Sprintf("Websocket connecting to %s", wsURL.String()))

	Connector.Mu.Lock()
	Connector.APIOrigin = apiOrigin
	h := http.Header{"Origin": []string{"http://jacktrip.local"}}
	c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), h)
	Connector.Conn = c
	Connector.Mu.Unlock()

	if err != nil {
		log.Error(err, "Websocket initialization error")
		Connector.IsInitialized = false
	} else {
		ReceivePingChannel = make(chan bool)
		go receivePingHandler()

		// Keep the socket alive
		keepAlive(Connector.Conn, 5*time.Minute)
		Connector.IsInitialized = true
		log.Info(fmt.Sprintf("Websocket connected to %s", wsURL.String()))
	}

	return err
}

func keepAlive(c *websocket.Conn, timeout time.Duration) {
	lastResponse := time.Now()
	c.SetPongHandler(func(msg string) error {
		lastResponse = time.Now()
		return nil
	})

	// Goroutine to keep the connection alive
	go func() {
		for {
			err := c.WriteMessage(websocket.PingMessage, []byte("keepalive"))
			if err != nil {
				return
			}
			time.Sleep(timeout / 2)
			if time.Now().Sub(lastResponse) > timeout {
				err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				CloseWSConnection()
				if err != nil {
					log.Error(err, "Closing socket")
					return
				}
				log.Info("Websocket timed out")
				return
			}
		}
	}()
}

// SendDevicePing sends a current ping statistics through Connector
func SendDevicePing(pingStats client.PingStats) {
	// encode ping content
	pingBytes, err := json.Marshal(pingStats)
	if err != nil {
		log.Error(err, "Failed to marshal agent ping request")
	}

	// effectively let this Goroutine time out in 3 seconds
	Connector.Conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	err = Connector.Conn.WriteMessage(websocket.TextMessage, pingBytes)
	if err != nil {
		log.Error(err, "Failed to send ping stats to the api server")
	}
}

func receivePingHandler() {
	defer close(ReceivePingChannel)
	for {
		select {
		case <-ReceivePingChannel:
			log.Info("Exiting ReceivePingHandler")
			return
		// case where nothing goes wrong
		default:
			var config client.AgentConfig
			_, message, err := Connector.Conn.ReadMessage()

			if err != nil {
				log.Error(err, "[WS] Error reading message. Closing the connection")
				CloseWSConnection()
				return
			}

			if err := json.Unmarshal(message, &config); err != nil {
				log.Error(err, "Failed to unmarshal agent ping response")
				continue
			}

			ConfigChannel <- config
		}
	}
}

// CloseWSConnection closes an existing connection
func CloseWSConnection() {
	Connector.IsInitialized = false
	Connector.Mu.Lock()
	Connector.Conn.Close()
	Connector.Mu.Unlock()
}
