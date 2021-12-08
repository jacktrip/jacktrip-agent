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
	MediaDir = "/tmp/vs-media"
)

// runHTTPServer runs the agent's HTTP server
func runHTTPServer(wg *sync.WaitGroup, router *mux.Router, address string) error {
	defer wg.Done()
	log.Info("Creating media directory")
	err := os.MkdirAll(MediaDir, os.ModePerm)
	if err != nil {
		log.Error(err, "Failed creating media directory")
	}
	log.Info("Starting an agent HTTP server")
	err = http.ListenAndServe(address, router)
	if err != nil {
		log.Error(err, "HTTP server error")
	}

	return err
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

// handleListenRequest loads the minimal set of elements for a live audio player
func handleListenRequest(w http.ResponseWriter, r *http.Request) {
	rawHTML := `
<html>
  <head>
    <title>Hls.js demo - basic usage</title>
  </head>
  <body style="background-color:black;">
    <script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
	  <center>
	    <video height="600" id="video" controls></video>
	  </center>
	<script>
	  // Source: https://github.com/dailymotion/hls.js/blob/master/demo/basic-usage.html
	  if(Hls.isSupported()) {
	    var video = document.getElementById('video');
		var hls = new Hls();
		hls.loadSource('/stream');
		hls.attachMedia(video);
		hls.on(Hls.Events.MANIFEST_PARSED,function() {
		  video.play();
		});
	  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
		video.src = '/stream';
		video.addEventListener('canplay',function() {
		  video.play();
		});
	  }
	</script>
  </body>
</html>
`
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(rawHTML))
}

// handleStreamRequest handles the initiation of a HLS stream
func handleStreamRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		id = "720p.m3u8"
	}
	serveHLS(w, r, MediaDir, id)
}

// serveHLS responds with proper media-encoded responses for HLS streams
func serveHLS(w http.ResponseWriter, r *http.Request, mediaBase, m3u8Name string) {
	mediaFile := fmt.Sprintf("%s/%s", mediaBase, m3u8Name)
	contentType := "video/MP2T"
	if strings.HasSuffix(mediaFile, ".m3u8") {
		contentType = "application/x-mpegURL"
	}
	http.ServeFile(w, r, mediaFile)
	w.Header().Set("Content-Type", contentType)
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
