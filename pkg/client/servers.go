package client

import (
	"github.com/jmoiron/sqlx/types"
)

// ServerType is used to determine the type of an audio server
type ServerType string

const (
	// JackTrip server (https://github.com/jacktrip/jacktrip)
	JackTrip ServerType = "JackTrip"

	// Jamulus server (https://github.com/corrados/jamulus)
	Jamulus ServerType = "Jamulus"

	// JackTripJamulus means both JackTrip AND Jamulus server
	JackTripJamulus ServerType = "JackTrip+Jamulus"
)

// ServerConfig defines configuration for a particular server
type ServerConfig struct {
	// type of server
	Type ServerType `json:"type" db:"type"`

	// Branch of jacktrip/jacktrip-sc repository to use for mixing
	MixBranch string `json:"mixBranch" db:"mix_branch"`

	// SuperCollider (sclang) source code to run for mixing audio
	MixCode string `json:"mixCode" db:"mix_code"`

	// hostname of server
	Host string `json:"serverHost" db:"host"`

	// port number server is listening on
	Port int `json:"serverPort" db:"port"`

	// sample rate frequency (48000 or 96000)
	SampleRate int `json:"sampleRate" db:"sample_rate"`

	// true if stereo, false if mono
	Stereo types.BitBool `json:"stereo" db:"stereo"`

	// true if client's audio should loop back to them
	LoopBack types.BitBool `json:"loopback" db:"loopback"`

	// true if enabled
	Enabled types.BitBool `json:"enabled" db:"enabled"`
}
