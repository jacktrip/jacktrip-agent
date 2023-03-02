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
	"time"

	"github.com/jmoiron/sqlx/types"
)

// ServerType is used to determine the type of an audio server
type ServerType string

// BroadcastVisibility controls the access modes of an audio server broadcast
type BroadcastVisibility int

const (
	// JackTrip server (https://github.com/jacktrip/jacktrip)
	JackTrip ServerType = "JackTrip"

	// Jamulus server (https://github.com/corrados/jamulus)
	Jamulus ServerType = "Jamulus"

	// JackTripJamulus means both JackTrip AND Jamulus server
	JackTripJamulus ServerType = "JackTrip+Jamulus"

	// Offline means broadcasting/recording is disabled
	Offline BroadcastVisibility = 0

	// BroadcastAndRecord means broadcasting/recording is enabled
	BroadcastAndRecord BroadcastVisibility = 1

	// BroadcastOnly means broadcasting is enabled for those with the link, but recording is disabled
	BroadcastOnly BroadcastVisibility = 2

	// RecordOnly means broadcasting is disabled but recording is enabled
	RecordOnly BroadcastVisibility = 3
)

// ServerConfig defines configuration for a particular server
type ServerConfig struct {
	// type of server
	Type ServerType `json:"type" db:"type"`

	// Descriptive name
	Name string `json:"name" db:"name"`

	// hostname of server
	Host string `json:"serverHost" db:"host"`

	// port number server is listening on
	Port int `json:"serverPort" db:"port"`

	// sample rate frequency (48000 or 96000)
	SampleRate int `json:"sampleRate" db:"sample_rate"`

	// true if server is publically accessible
	Public types.BitBool `json:"public" db:"public"`

	// DEPRECATED: should always be true
	Stereo types.BitBool `json:"stereo" db:"stereo"`

	// DEPRECATED: should always be false
	LoopBack types.BitBool `json:"loopback" db:"loopback"`

	// true if enabled
	Enabled types.BitBool `json:"enabled" db:"enabled"`
}

// ServerAgentConfig defines active configuration for a server
type ServerAgentConfig struct {
	ServerConfig

	// frames per period
	Period int `json:"period" db:"period"`

	// size of jitter queue buffer
	QueueBuffer int `json:"queueBuffer" db:"queue_buffer"`

	// strategy to use for the network jitter buffer
	BufferStrategy int `json:"bufferStrategy" db:"buffer_strategy"`

	// broadcast visibility of the audio server
	Broadcast BroadcastVisibility `json:"broadcast" db:"broadcast"`

	// timestamp when the server will automatically be paused
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`

	// Branch of jacktrip/jacktrip-sc repository to use for mixing
	MixBranch string `json:"mixBranch" db:"mix_branch"`

	// SuperCollider (sclang) source code to run for mixing audio
	MixCode string `json:"mixCode" db:"mix_code"`

	// max number of musicians allowed in server
	MaxMusicians int `json:"maxMusicians" db:"max_musicians"`
}

// ServerHeartbeat is used to send heartbeat messages from servers / studios
type ServerHeartbeat struct {
	// Cloud identifier for server (used when running on cloud audio server)
	CloudID string `json:"cloudId"`
}
