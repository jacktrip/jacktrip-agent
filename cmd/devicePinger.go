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
	"math"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

// Recorder is used as a singleton instance to keep track of RTT times and the latest stats
type Recorder struct {
	RttEpochTimes []time.Duration
	Stats         client.PingStats
}

// PingRecorderLimit sets the capacity of the RTT array in PingRecorder
var PingRecorderLimit = 5
// PingRecorder is a singleton instance of Recorder used globally
var PingRecorder = Recorder{}

// PingAudioServer uses a socket connection to measure a RTT to an audio server
func PingAudioServer(apiOrigin string, host string, port int) {
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("%s:%d", host, port), Path: "/ping"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Error(err, fmt.Sprintf("Could not reach the audio server at %s", u.String()))
		return
	}
	defer c.Close()

	for i := 0; i < PingRecorderLimit; i++ {
		// Write the current timestamp in nanoseconds
		start := time.Now()
		err := c.WriteMessage(websocket.TextMessage, []byte("a"))
		if err != nil {
			log.Error(err, "Could not write the message to the audio server")
			return
		}

		c.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Error(err, "Could not read the message from the audio server")
			return
		}

		if message[0] == 97 {
			PingRecorder.addPingRecord(time.Since(start))
		}

		time.Sleep(1 * time.Second)
	}
}

func (*Recorder) addPingRecord(pingRecord time.Duration) {
	if len(PingRecorder.RttEpochTimes) >= PingRecorderLimit {
		PingRecorder.RttEpochTimes = append(PingRecorder.RttEpochTimes[1:], pingRecord)
	} else {
		PingRecorder.RttEpochTimes = append(PingRecorder.RttEpochTimes, pingRecord)
	}
	PingRecorder.calculateStats()
}

func (*Recorder) calculateStats() {
	var total, minRtt, maxRtt, avgRtt, sd time.Duration
	for _, rtt := range PingRecorder.RttEpochTimes {
		total += rtt
		if rtt < minRtt || minRtt == time.Duration(0) {
			minRtt = rtt
		}
		if rtt > maxRtt {
			maxRtt = rtt
		}
	}
	avgRtt = time.Duration(total.Nanoseconds() / int64(len(PingRecorder.RttEpochTimes)))

	for _, rtt := range PingRecorder.RttEpochTimes {
		sd += (rtt - avgRtt) * (rtt - avgRtt)
	}
	PingRecorder.Stats = client.PingStats{}
	PingRecorder.Stats.MinRtt = minRtt
	PingRecorder.Stats.MaxRtt = maxRtt
	PingRecorder.Stats.AvgRtt = avgRtt
	PingRecorder.Stats.StdDevRtt = time.Duration(math.Sqrt(float64(sd.Nanoseconds() / int64(len(PingRecorder.RttEpochTimes)))))
	PingRecorder.Stats.LatestRtt = PingRecorder.RttEpochTimes[len(PingRecorder.RttEpochTimes)-1]
}

// Reset clears the attributes of PingRecorder
func (*Recorder) Reset() {
	PingRecorder.RttEpochTimes = []time.Duration{}
	PingRecorder.Stats = client.PingStats{}
}
