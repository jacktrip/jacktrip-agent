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
	"os"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	// MediaDir is the filepath to where jacktrip-agent stores local media files for recording/streaming
	MediaDir = "/tmp/vs-media"
)

// runHTTPServer runs the agent's HTTP server
func runHTTPServer(wg *sync.WaitGroup, router *mux.Router, address string) error {
	defer wg.Done()
	createMediaDirectory()
	log.Info("Starting an agent HTTP server")
	err := http.ListenAndServe(address, router)
	if err != nil {
		log.Error(err, "HTTP server error")
	}

	return err
}

func createMediaDirectory() {
	log.Info("Creating media directory")
	os.RemoveAll(MediaDir)
	err := os.MkdirAll(MediaDir, os.ModePerm)
	if err != nil {
		log.Error(err, "Failed creating media directory")
	}
}

// handlePingRequest upgrades ping request to a websocket responder
func handlePingRequest(w http.ResponseWriter, r *http.Request) {
	// return success if no request for websocket
	if r.Header.Get("Connection") != "Upgrade" || r.Header.Get("Upgrade") != "websocket" {
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

// verifyToken checks the request headers for a valid bearer token
func verifyToken(w http.ResponseWriter, r *http.Request) bool {
	if serverToken == "" {
		return true
	}
	reqToken := r.Header.Get("Authorization")
	if reqToken != "" {
		splitToken := strings.Split(reqToken, "Bearer ")
		reqToken = splitToken[1]
		if serverToken == reqToken {
			return true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

// handleStreamRequest handles the initiation of a HLS stream
func handleStreamRequest(w http.ResponseWriter, r *http.Request) {
	if !verifyToken(w, r) {
		return
	}
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		id = "index.m3u8"
	}
	serveHLS(w, r, MediaDir, id)
}

// checkHLSOrigin checks for allowed origins in an HLS request
func checkHLSOrigin(requestOrigin string) bool {
	allowedOrigins := [...]string{"https://jacktrip.radio", "https://app.jacktrip.org", "https://test.jacktrip.radio", "https://test.jacktrip.org", "http://localhost:3000", "http://localhost:8000"}
	for _, v := range allowedOrigins {
		if v == requestOrigin {
			return true
		}
	}
	return false
}

// serveHLS responds with proper media-encoded responses for HLS streams
func serveHLS(w http.ResponseWriter, r *http.Request, mediaBase, m3u8Name string) {
	mediaFile := fmt.Sprintf("%s/%s", mediaBase, m3u8Name)
	contentType := "video/MP2T"
	if strings.HasSuffix(mediaFile, ".m3u8") {
		contentType = "application/vnd.apple.mpegurl"
	} else if strings.HasSuffix(mediaFile, ".mp4") || strings.HasSuffix(mediaFile, ".m4s") {
		contentType = "video/iso.segment"
	}

	requestOrigin := r.Header.Get("Origin")
	if requestOrigin != "" && checkHLSOrigin(requestOrigin) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, mediaFile)
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
