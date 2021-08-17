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
	"net/url"
	"sync"
	"time"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

type WebSocketConnector struct {
	Conn *websocket.Conn
	Mu sync.Mutex
	ApiOrigin string
	IsInitialized bool
	DeviceId string
}

var Connector WebSocketConnector
var ConfigChannel = make(chan client.AgentConfig, 100)
var ReceivePingChannel chan bool

func FormatDevicePingURL(id string) string {
	var devicePingURL = "/agents/ping/%s"
	return fmt.Sprintf(devicePingURL, id)
}

func InitWsConnection(apiOrigin string, ping client.AgentPing) (WebSocketConnector, error) {
	var err error
	if Connector.IsInitialized == true {
		return Connector, err
	} 

	u, _ := url.Parse(apiOrigin)
	wsURL := url.URL{Scheme: "wss", Host: u.Host, Path: fmt.Sprintf("%s%s", u.Path, FormatDevicePingURL(ping.MAC))}
	log.Info(fmt.Sprintf("Websocket connecting to %s", wsURL.String()))

	Connector.Mu.Lock()
	Connector.ApiOrigin = apiOrigin
	h := http.Header{"Origin": []string{"http://jacktrip.local"}}
	c, _, err := websocket.DefaultDialer.Dial(wsURL.String(), h)
	Connector.Conn = c
	Connector.Mu.Unlock()
	if err != nil {
		log.Error(err, "Websocket initialization error")
		Connector.IsInitialized = false
	} else {
		ReceivePingChannel = make(chan bool)
		go ReceivePingHandler()

		// Keep the socket alive
		keepAlive(Connector.Conn, 5 * time.Minute)
		Connector.IsInitialized = true
		log.Info(fmt.Sprintf("Websocket connected to %s", wsURL.String()))
	}

	return Connector, err
}

func keepAlive(c *websocket.Conn, timeout time.Duration) {
    lastResponse := time.Now()
    c.SetPongHandler(func(msg string) error {
    	lastResponse = time.Now()
    	return nil
	})

	// goroutine to keep the connection alive
	go func() {
		for {
			err := c.WriteMessage(websocket.PingMessage, []byte("keepalive"))
			if err != nil {
				return 
			}
			time.Sleep(timeout/2)
			if(time.Now().Sub(lastResponse) > timeout) {
				err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				CloseConnection()
				if err != nil {
					log.Error(err, "Closing socket error")
					return
				}
				log.Info("Websocket timed out")
				return
			}
		}
	}()
}

func SendPing(ping client.AgentPing) error {
	// update and encode ping content
	pingBytes, err := json.Marshal(ping)
	if err != nil {
		log.Error(err, "Failed to marshal agent ping request")
		return err
	}

	Connector.Conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	err = Connector.Conn.WriteMessage(websocket.TextMessage, pingBytes)
	if err != nil {
		log.Error(err, "[WS] Error writing message")
		CloseConnection()
	}
	return err
}

// Use ReceivePingHandler as a goroutine
func ReceivePingHandler() {
	for {
		select {
		case <- ReceivePingChannel:
			log.Info("Exiting ReceivePingHandler")
			return
		// case where nothing goes wrong
		default:
			var config client.AgentConfig
			log.Info("Reading happening")
			_, message, err := Connector.Conn.ReadMessage()
			
			if err != nil {
				log.Error(err, "[WS] Error reading message. Closing the connection")
				CloseConnection()
				return
			}

			// device id message comes in a form of "ID:{deviceId}"
			if strings.Contains(string(message), "ID:") {
				Connector.DeviceId = string(message)[4:]
				continue
			}

			// log.Info(fmt.Sprintf("******** RCV: ", string(message)))

			if err := json.Unmarshal(message, &config); err != nil {
				log.Error(err, "Failed to unmarshal agent ping response")
				continue
			}

			log.Info(fmt.Sprintf("Straight from the socket %#v\n", config))

			ConfigChannel <- config
			log.Info("Received config through websocket")
		}
	}
}

func CloseConnection() {
	Connector.IsInitialized = false
	Connector.Mu.Lock()
	Connector.Conn.Close()
	Connector.Mu.Unlock()
}
