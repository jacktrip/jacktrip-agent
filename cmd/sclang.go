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
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// SuperColliderDefaultCode is the default startup code used for supercollider as a fallback when things fail
	SuperColliderDefaultCode = "SimpleMixer(~maxClients).after({0.exit;}).connect.start;\n"
)

// SCOption is a supercollider configuration option, which may contain one or more variables
type SCOption struct {
	// Name of a specific supercollider configuration variable
	Name string `json:"name"`

	// Value of a specific supercollider configuration variable
	Value float32 `json:"value"`

	// Names is a slice of supercollider configuration variable names
	Names []string `json:"names"`

	// Values is a slice of supercollider configuration variable values
	Values []float32 `json:"values"`
}

// marshalSCVariable marshals a single supercollider variable into a string
func marshalSCVariable(name string, value float32) string {
	if value == float32(int(value)) {
		// don't include decimal if it's zero
		return fmt.Sprintf(".%s_(%d)", name, int(value))
	}
	return fmt.Sprintf(".%s_(%f)", name, value)
}

// Marshal is a method of SCOption that marshals it into a byte array
func (v *SCOption) Marshal() ([]byte, error) {
	var sb strings.Builder

	if v.Name != "" {
		if _, err := sb.WriteString(marshalSCVariable(v.Name, v.Value)); err != nil {
			return nil, err
		}
	}

	for n := range v.Names {
		var myValue float32
		if len(v.Values) > n {
			myValue = v.Values[n]
		}
		if _, err := sb.WriteString(marshalSCVariable(v.Names[n], myValue)); err != nil {
			return nil, err
		}
	}

	return []byte(sb.String()), nil
}

// SCLink is a singal chain processing link used in supercollider configurations
type SCLink struct {
	// Name of the Link
	Name string `json:"name"`

	// Options for configuration of Link
	Options []SCOption `json:"options"`
}

// Marshal is a method of SCLang that marshals it into a byte array
func (l *SCLink) Marshal() ([]byte, error) {
	var sb strings.Builder

	if _, err := sb.WriteString(fmt.Sprintf("%s()", l.Name)); err != nil {
		return nil, err
	}

	for _, opt := range l.Options {
		optBytes, _ := opt.Marshal()
		if _, err := sb.Write(optBytes); err != nil {
			return nil, err
		}
	}

	return []byte(sb.String()), nil
}

// SCSignalChain is used to represent an audio processing signal chain for a supercollider server
type SCSignalChain []SCLink

// Marshal is a method of SCSignalChain that marshals it into a byte array
func (c *SCSignalChain) Marshal() ([]byte, error) {
	var sb strings.Builder

	if _, err := sb.WriteString("SignalChain(~maxClients)"); err != nil {
		return nil, err
	}

	for _, link := range *c {
		linkBytes, _ := link.Marshal()
		if _, err := sb.WriteString(fmt.Sprintf("\n.append(%s)", string(linkBytes))); err != nil {
			return nil, err
		}
	}

	return []byte(sb.String()), nil
}

// SCConfig is used to represent the audio processing config for a supercollider server
type SCConfig struct {
	// Mixer is the name of the audio mixer class to use
	Mixer string `json:"mixer"`

	// MixerOptions are the mixer configuration options
	MixerOptions []SCOption `json:"mixerOptions"`

	// PreChain is used to process audio before mixing down to 2 channels
	PreChain SCSignalChain `json:"preChain"`

	// PostChain is used to process audio after mixing down to 2 channels
	PostChain SCSignalChain `json:"postChain"`

	// SCLang represents the SCLang custom code when useCustomCode is true
	SCLang string `json:"sclang"`

	// UseCustomCode indicates if custom code in the SCLang parameter should be used
	UseCustomCode bool `json:"useCustomCode"`
}

// Marshal is a method of SCConfig that marshals it into a byte array
func (config *SCConfig) Marshal() ([]byte, error) {
	var sb strings.Builder
	var err error
	var preChainBytes, postChainBytes []byte

	// check custom code
	if config.UseCustomCode {
		// use default if empty
		if config.SCLang == "" {
			return []byte(SuperColliderDefaultCode), nil
		}
		return []byte(config.SCLang), nil
	}

	// ~preChain
	if preChainBytes, err = config.PreChain.Marshal(); err != nil {
		return nil, err
	}
	if _, err := sb.WriteString("~preChain = "); err != nil {
		return nil, err
	}
	if _, err := sb.Write(preChainBytes); err != nil {
		return nil, err
	}

	// ~postChain
	if postChainBytes, err = config.PostChain.Marshal(); err != nil {
		return nil, err
	}
	if _, err := sb.WriteString(";\n\n~postChain = "); err != nil {
		return nil, err
	}
	if _, err := sb.Write(postChainBytes); err != nil {
		return nil, err
	}

	// ~mixer
	if _, err := sb.WriteString(fmt.Sprintf(";\n\n~mixer = %s(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain)", config.Mixer)); err != nil {
		return nil, err
	}
	for _, opt := range config.MixerOptions {
		optBytes, _ := opt.Marshal()
		if _, err := sb.Write(optBytes); err != nil {
			return nil, err
		}
	}
	if _, err := sb.WriteString(";\n\n~mixer.connect.start;\n"); err != nil {
		return nil, err
	}

	return []byte(sb.String()), nil
}

// generateSuperColliderCode is used to translate the mixCode config parm into sclang
func generateSuperColliderCode(mixCode string) string {
	var config SCConfig
	var err error

	if mixCode == "" {
		log.Error(err, "mixCode is empty; falling back to default")
		return SuperColliderDefaultCode
	}

	if err = json.Unmarshal([]byte(mixCode), &config); err != nil {
		log.Info("Failed to parse mixCode as JSON; using as-is for backwards compatability")
		return mixCode
	}

	scBytes, err := config.Marshal()
	if err != nil {
		log.Error(err, "Failed generate SCLang from mixCode; falling back to default")
		return SuperColliderDefaultCode
	}

	return string(scBytes)
}
