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

// handleListenRequest loads the minimal set of elements for a live audio player
func handleListenRequest(w http.ResponseWriter, r *http.Request) {
	rawHTML := `
<html>
  <head>
    <meta charset="utf-8">
    <title>HLS</title>
    <link href="https://vjs.zencdn.net/7.17.0/video-js.css" rel="stylesheet" />
  </head>
  <body>
    <script src="https://vjs.zencdn.net/7.17.0/video.min.js"></script>
    <script src="https://esonderegger.github.io/web-audio-peak-meter/web-audio-peak-meter-2.0.0.min.js"></script>
    <div id="my-peak-meter" style="width: 20em; height: 5em; margin: 1em 0;"></div>
    <video-js crossorigin="anonymous" id="vid1" width=600 height=300 class="vjs-default-skin" controls>
      <source src="/stream/index.m3u8" type="application/x-mpegURL">
    </video-js>
    <script>
      var player = videojs('vid1');
      player.play();
    </script>
    <script>
      var myMeterElement = document.getElementById('my-peak-meter');
      var myAudio = document.getElementsByTagName('video')[0];
	  console.log(myAudio)
      var audioCtx = new window.AudioContext();
      var sourceNode = audioCtx.createMediaElementSource(myAudio);
      sourceNode.connect(audioCtx.destination);
      var meterNode = webAudioPeakMeter.createMeterNode(sourceNode, audioCtx);
      webAudioPeakMeter.createMeter(myMeterElement, meterNode, {});
      myAudio.addEventListener('play', function() {
        audioCtx.resume();
      });
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
		id = "index.m3u8"
	}
	serveHLS(w, r, MediaDir, id)
}

// serveHLS responds with proper media-encoded responses for HLS streams
func serveHLS(w http.ResponseWriter, r *http.Request, mediaBase, m3u8Name string) {
	mediaFile := fmt.Sprintf("%s/%s", mediaBase, m3u8Name)
	contentType := "video/MP2T"
	if strings.HasSuffix(mediaFile, ".m3u8") {
		contentType = "application/x-mpegURL"
	} else if strings.HasSuffix(mediaFile, ".mp4") {
		contentType = "video/mp4"
	} else if strings.HasSuffix(mediaFile, ".m4s") {
		contentType = "video/mp4"
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
