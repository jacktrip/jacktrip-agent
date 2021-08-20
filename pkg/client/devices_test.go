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

package client

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestDeviceConfig(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target DeviceConfig

	// Parse JSON into DeviceConfig struct
	raw = `{"devicePort": 8000, "reverb": 42, "limiter": true, "compressor": 0, "quality": 2}`
	target = DeviceConfig{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(8000, target.DevicePort)
	assert.Equal(42, target.Reverb)
	assert.Equal(true, bool(target.Limiter))
	assert.Equal(false, bool(target.Compressor))
	assert.Equal(2, target.Quality)
}

func TestALSAConfig(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target ALSAConfig

	// Parse JSON into ALSAConfig struct
	raw = `{"captureBoost": true, "playbackBoost": 0, "captureVolume": 100, "playbackVolume": 0}`
	target = ALSAConfig{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(true, bool(target.CaptureBoost))
	assert.Equal(false, bool(target.PlaybackBoost))
	assert.Equal(100, target.CaptureVolume)
	assert.Equal(0, target.PlaybackVolume)
}

func TestPingStats(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target PingStats

	// Parse JSON into PingStats struct
	raw = `{"pkts_recv": 832, "pkts_sent": 3, "min_rtt": 3, "max_rtt": -5, "avg_rtt": 301, "stddev_rtt": -10291, "stats_updated_at": "2021-08-11T10:28:32.487013776-07:00"}`
	target = PingStats{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(832, target.PacketsRecv)
	assert.Equal(3, target.PacketsSent)
	assert.Equal(time.Duration(3), target.MinRtt)
	assert.Equal(-1*time.Duration(5), target.MaxRtt)
	assert.Equal(time.Duration(301), target.AvgRtt)
	assert.Equal(-1*time.Duration(10291), target.StdDevRtt)
	assert.Equal("2021-08-11 10:28:32.487013776 -0700 PDT", target.StatsUpdatedAt.String())
}

func TestAgentConfig(t *testing.T) {
	t.Skip("TODO")
}

func TestAgentCredentials(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target AgentCredentials

	// Parse JSON into AgentCredentials struct
	raw = `{"apiPrefix": "black", "apiSecret": "pink"}`
	target = AgentCredentials{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal("black", target.APIPrefix)
	assert.Equal("pink", target.APISecret)
}

func TestGetAPIHash(t *testing.T) {
	assert := assert.New(t)
	result := GetAPIHash("blackpink")
	assert.Equal("b13dabc4285540382af3f280bfc55c0752806a177f896afa8ec568b0206c3bf5", result)
}

func TestAgentPing(t *testing.T) {
	t.Skip("TODO")
}
