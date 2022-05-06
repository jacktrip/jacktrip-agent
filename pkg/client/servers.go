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

	// Private means no one has access
	Private BroadcastVisibility = 0

	// Public means everyone has access
	Public BroadcastVisibility = 1

	// Unlisted means only people with the link have access
	Unlisted BroadcastVisibility = 2
)

// ServerConfig defines configuration for a particular server
type ServerConfig struct {
	// type of server
	Type ServerType `json:"type" db:"type"`

	// Branch of jacktrip/jacktrip-sc repository to use for mixing
	MixBranch string `json:"mixBranch" db:"mix_branch"`

	// SuperCollider (sclang) source code to run for mixing audio
	MixCode string `json:"mixCode" db:"mix_code"`

	// max number of musicians allowed in server
	MaxMusicians int `json:"maxMusicians" db:"max_musicians"`

	// hostname of server
	Host string `json:"serverHost" db:"host"`

	// port number server is listening on
	Port int `json:"serverPort" db:"port"`

	// sample rate frequency (48000 or 96000)
	SampleRate int `json:"sampleRate" db:"sample_rate"`

	// true if stereo, false if mono
	// this field will be replaced by deviceConfig's input and output channels
	// don't delete it for backward compatibility
	Stereo types.BitBool `json:"stereo" db:"stereo"`

	// true if client's audio should loop back to them
	LoopBack types.BitBool `json:"loopback" db:"loopback"`

	// broadcast visibility of the audio server
	Broadcast BroadcastVisibility `json:"broadcast" db:"broadcast"`

	// true if enabled
	Enabled types.BitBool `json:"enabled" db:"enabled"`
}

// ServerHeartbeat is used to send heartbeat messages from servers / studios
type ServerHeartbeat struct {
	// Cloud identifier for server (used when running on cloud audio server)
	CloudID string `json:"cloudId"`
}
