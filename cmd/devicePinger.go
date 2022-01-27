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
	"math"
	"net/url"
	"time"

	goping "github.com/go-ping/ping"
	"github.com/gorilla/websocket"
	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

// MeasurePingStats uses a socket connection to measure a RTT to an audio server
func MeasurePingStats(beat *client.DeviceHeartbeat, apiOrigin string, host string) {
	u := url.URL{Scheme: "wss", Host: host, Path: "/ping"}
	dialer := websocket.Dialer{HandshakeTimeout: time.Second}
	c, _, err := dialer.Dial(u.String(), nil) // this may block for HandshakeTimeout if the connection fails

	// If a socket connection does not work for the host, use a ICMP ping (note: this normally should take 4 seconds)
	if err != nil {
		// Run icmp ping
		pinger, err := goping.NewPinger(host)
		if err != nil {
			log.Error(err, "Failed to create a icmp pinger")
			return
		}

		pinger.Count = HeartbeatInterval
		pinger.Interval = time.Second
		pinger.Timeout = HeartbeatInterval * time.Second
		pinger.Run() // blocking until done
		updateICMPPing(beat, pinger.Statistics())
		log.V(1).Info("Updated device heartbeat with ICMP ping result")
		return
	}

	// Use an established socket connection for RTT measurement
	defer c.Close()

	var socketRtts []time.Duration
	for i := 0; i < HeartbeatInterval; i++ {
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
			socketRtts = append(socketRtts, time.Since(start))
		}

		time.Sleep(time.Second)
	}
	updateWSPing(beat, socketRtts)
	log.V(1).Info("Updated device heartbeat with websocket ping result")
}

// updatePing function takes icmpStats object and update ping statistics
func updateICMPPing(beat *client.DeviceHeartbeat, icmpStats *goping.Statistics) {
	beat.MinRtt = icmpStats.MinRtt
	beat.MaxRtt = icmpStats.MaxRtt
	beat.AvgRtt = icmpStats.AvgRtt
	beat.StdDevRtt = icmpStats.StdDevRtt
	if len(icmpStats.Rtts) > 0 {
		beat.LatestRtt = icmpStats.Rtts[len(icmpStats.Rtts)-1]
	}
	beat.PacketsSent = icmpStats.PacketsSent
	beat.PacketsRecv = icmpStats.PacketsRecv
	beat.StatsUpdatedAt = time.Now()
}

// updateWSPing takes rtt array to update ping statistics
func updateWSPing(beat *client.DeviceHeartbeat, rtts []time.Duration) {
	var total, minRtt, maxRtt, avgRtt, sd time.Duration
	for _, rtt := range rtts {
		total += rtt
		if rtt < minRtt || minRtt == time.Duration(0) {
			minRtt = rtt
		}
		if rtt > maxRtt {
			maxRtt = rtt
		}
	}

	avgRtt = time.Duration(total.Nanoseconds() / int64(len(rtts)))

	for _, rtt := range rtts {
		sd += (rtt - avgRtt) * (rtt - avgRtt)
	}
	beat.MinRtt = minRtt
	beat.MaxRtt = maxRtt
	beat.AvgRtt = avgRtt
	beat.StdDevRtt = time.Duration(math.Sqrt(float64(sd.Nanoseconds() / int64(len(rtts)))))
	if len(rtts) > 0 {
		beat.LatestRtt = rtts[len(rtts)-1]
	}
	beat.PacketsSent = HeartbeatInterval
	beat.PacketsRecv = len(rtts)
	beat.StatsUpdatedAt = time.Now()
}
