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
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// runHTTPServer runs the agent's HTTP server
func runHTTPServer(wg *sync.WaitGroup, router *mux.Router, address string) *http.Server {
	log.Info("Starting agent HTTP server")
	srv := &http.Server{Addr: address, Handler: router}

	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Error(err, "HTTP server error")
		}
	}()

	log.Info("Successfully started agent HTTP server")
	return srv
}

// shutdownHTTPServer gracefully terminates the HTTP server
func shutdownHTTPServer(server *http.Server) {
	// use a separate context to enforce server shutdown within time limit
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
	}()
	if err := server.Shutdown(ctx); err != nil {
		panic(err)
	}
}

// handlePingRequest upgrades ping request to a websocket responder
func handlePingRequest(w http.ResponseWriter, r *http.Request) {
	// return success if no request for websocket
	if strings.ToLower(r.Header.Get("Connection")) != "upgrade" || strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
		return
	}

	// upgrade to websocket
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err, "Unable to upgrade to websocket")
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error(err, "Unable to read websocket message")
			}
			break
		}
		err = c.WriteMessage(mt, message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error(err, "Unable to write websocket message")
			}
			break
		}
	}
}

// OptionsGetOnly responds with a list of allow methods for Get only
func OptionsGetOnly(w http.ResponseWriter, r *http.Request) {
	allowMethods := "GET, OPTIONS"
	w.Header().Set("Allow", allowMethods)
	w.Header().Set("Access-Control-Allow-Methods", allowMethods)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
}

// RespondJSON makes the response with payload as json format
func RespondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	w.Write([]byte(response))
}
