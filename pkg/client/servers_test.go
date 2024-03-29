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

package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstants(t *testing.T) {
	assert := assert.New(t)
	assert.Equal(ServerType("JackTrip"), JackTrip)
	assert.Equal(ServerType("Jamulus"), Jamulus)
	assert.Equal(ServerType("JackTrip+Jamulus"), JackTripJamulus)
}

func TestServerConfig(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target ServerConfig

	// Parse JSON into ServerConfig struct
	raw = `{"type": "JackTrip+Jamulus", "serverHost": "a.b.com", "serverPort": 8000, "sampleRate": 96000, "enabled": true}`
	target = ServerConfig{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(JackTripJamulus, target.Type)
	assert.Equal("a.b.com", target.Host)
	assert.Equal(8000, target.Port)
	assert.Equal(96000, target.SampleRate)
	assert.Equal(true, bool(target.Enabled))
}

func TestServerAgentConfig(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target ServerAgentConfig

	// Parse JSON into ServerAgentConfig struct
	raw = `{"type": "JackTrip+Jamulus", "name": "Test Server", "mixBranch": "main", "mixCode": "echo hi", "serverHost": "a.b.com", "serverPort": 8000, "sampleRate": 96000, "broadcast": 1, "public": true, "enabled": true}`
	target = ServerAgentConfig{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(JackTripJamulus, target.Type)
	assert.Equal("Test Server", target.Name)
	assert.Equal("echo hi", target.MixCode)
	assert.Equal("a.b.com", target.Host)
	assert.Equal(8000, target.Port)
	assert.Equal(96000, target.SampleRate)
	assert.Equal(true, bool(target.Public))
	assert.Equal(true, bool(target.Enabled))
	assert.Equal(BroadcastPublicWOStemWOVideo, target.Broadcast)
	assert.Equal("main", target.MixBranch)
	assert.Equal("echo hi", target.MixCode)
}

func TestServerHeartbeat(t *testing.T) {
	assert := assert.New(t)
	var raw string
	var target ServerHeartbeat

	// Parse JSON into DeviceHeartbeat struct
	raw = `{"cloudId": "aws"}`
	target = ServerHeartbeat{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal("aws", target.CloudID)
}
