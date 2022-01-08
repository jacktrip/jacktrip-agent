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
	"testing"

	"github.com/stretchr/testify/assert"
)

type SCMarshaller interface {
	Marshal() ([]byte, error)
}

func testMarshal(assert *assert.Assertions, obj SCMarshaller, expected string) {
	b, err := obj.Marshal()
	assert.Nil(err)
	assert.Equal(len(expected), len(b))
	assert.Equal(expected, string(b))
}

func testSCVariableUnmarshal(assert *assert.Assertions, raw string, name string, value float32) {
	target := SCVariable{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(name, target.Name)
	assert.Equal(value, target.Value)
}

func testSCLinkUnmarshal(assert *assert.Assertions, raw string, expected SCLink) {
	target := SCLink{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(expected.Name, target.Name)
	assert.Equal(len(expected.Options), len(target.Options))
	assert.Equal(expected.Options, target.Options)
}

func testSCSignalChainUnmarshal(assert *assert.Assertions, raw string, expected SCSignalChain) {
	target := SCSignalChain{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(len(expected), len(target))
	assert.Equal(expected, target)
}

func testSCConfigUnmarshal(assert *assert.Assertions, raw string, expected SCConfig) {
	target := SCConfig{}
	json.Unmarshal([]byte(raw), &target)
	assert.Equal(expected.Mixer, target.Mixer)
	assert.Equal(len(expected.MixerOptions), len(target.MixerOptions))
	assert.Equal(expected.MixerOptions, target.MixerOptions)
	assert.Equal(len(expected.PreChain), len(target.PreChain))
	assert.Equal(expected.PreChain, target.PreChain)
	assert.Equal(len(expected.PostChain), len(target.PostChain))
	assert.Equal(expected.PostChain, target.PostChain)
	assert.Equal(len(expected.SCLang), len(target.SCLang))
	assert.Equal(expected.SCLang, target.SCLang)
	assert.Equal(expected.UseCustomCode, target.UseCustomCode)
}

func TestSCVariable(t *testing.T) {
	assert := assert.New(t)
	testMarshal(assert, &SCVariable{Name: "zero", Value: 0}, ".zero_(0)")
	testMarshal(assert, &SCVariable{Name: "answer", Value: 42}, ".answer_(42)")
	testMarshal(assert, &SCVariable{Name: "float", Value: 0.242}, ".float_(0.242000)")
	testMarshal(assert, &SCVariable{Name: "negInt", Value: -21}, ".negInt_(-21)")
	testMarshal(assert, &SCVariable{Name: "negFloat", Value: -4.2}, ".negFloat_(-4.200000)")
	testSCVariableUnmarshal(assert, `{"varName": "zero", "value": 0}`, "zero", 0)
	testSCVariableUnmarshal(assert, `{"varName": "answer", "value": 42}`, "answer", 42)
	testSCVariableUnmarshal(assert, `{"varName": "float", "value": 0.242}`, "float", 0.242)
	testSCVariableUnmarshal(assert, `{"varName": "negInt", "value": -21}`, "negInt", -21)
	testSCVariableUnmarshal(assert, `{"varName": "negFloat", "value": -4.2}`, "negFloat", -4.2)
}

func TestSCLink(t *testing.T) {
	assert := assert.New(t)
	link := SCLink{
		Name: "myLink",
		Options: []SCVariable{
			{Name: "zero", Value: 0},
			{Name: "negFloat", Value: -4.2},
		},
	}
	testMarshal(assert, &link, "myLink().zero_(0).negFloat_(-4.200000)")
	testSCLinkUnmarshal(assert, `{"name": "myLink", "options": [{"varName": "zero", "value": 0}, {"varName": "negFloat", "value": -4.2}]}`, link)
}

func TestSCSignalChain(t *testing.T) {
	assert := assert.New(t)
	chain := SCSignalChain{
		{
			Name: "myLink",
			Options: []SCVariable{
				{Name: "float", Value: 0.242},
			},
		},
		{
			Name: "yourLink",
			Options: []SCVariable{
				{Name: "answer", Value: 42},
			},
		},
	}
	testMarshal(assert, &chain, "SignalChain(~maxClients)\n.append(myLink().float_(0.242000))\n.append(yourLink().answer_(42))")
	testSCSignalChainUnmarshal(assert, `[{"name":"myLink","options":[{"varName": "float", "value": 0.242}]},{"name":"yourLink","options":[{"varName": "answer", "value": 42}]}]`, chain)
}

func TestSCConfigAdvanced(t *testing.T) {
	assert := assert.New(t)

	// test empty advanced code => default sclang
	testMarshal(assert, &SCConfig{UseCustomCode: true}, SuperColliderDefaultCode)

	// test advanced code JSON parsing & pass-through
	advancedCode := "AdvancedMixer(~maxClients).after({0.exit;}).connect.start;"
	config := SCConfig{
		SCLang:        advancedCode,
		UseCustomCode: true,
	}
	testMarshal(assert, &config, advancedCode)
	testSCConfigUnmarshal(assert, fmt.Sprintf(`{"scLang":"%s","useCustomCode":true}`, advancedCode), config)
}

func TestSCConfigSimple(t *testing.T) {
	assert := assert.New(t)
	config := SCConfig{
		Mixer: "SimpleMixer",
		MixerOptions: []SCVariable{
			{"master", 100},
		},
	}

	testMarshal(assert, &config, `~preChain = SignalChain(~maxClients);

~postChain = SignalChain(~maxClients);

~mixer = SimpleMixer(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain).master_(100);

~mixer.connect.start;
`)

	testSCConfigUnmarshal(assert, `{"mixer":"SimpleMixer","masterOptions":[{"varName":"master","value":100}]}`, config)
}

func TestSCConfigWithSignalChains(t *testing.T) {
	assert := assert.New(t)
	config := SCConfig{
		Mixer: "PersonalMixer",
		PreChain: SCSignalChain{
			{
				Name: "myLink",
				Options: []SCVariable{
					{Name: "float", Value: 0.242},
				},
			},
		},
		PostChain: SCSignalChain{
			{
				Name: "yourLink",
				Options: []SCVariable{
					{Name: "answer", Value: 42},
				},
			},
		},
	}

	testMarshal(assert, &config, `~preChain = SignalChain(~maxClients)
.append(myLink().float_(0.242000));

~postChain = SignalChain(~maxClients)
.append(yourLink().answer_(42));

~mixer = PersonalMixer(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain);

~mixer.connect.start;
`)

	testSCConfigUnmarshal(assert, `{
		"mixer" : "PersonalMixer",
		"preChain" : [{"name":"myLink","options":[{"varName": "float", "value": 0.242}]}],
		"postChain" : [{"name":"yourLink","options":[{"varName": "answer", "value": 42}]}]
	}`, config)
}

func TestGenerateSuperColliderCodeDefault(t *testing.T) {
	assert := assert.New(t)
	assert.Equal(SuperColliderDefaultCode, generateSuperColliderCode(""))
}

func TestGenerateSuperColliderCodeBackwardsCompatability(t *testing.T) {
	assert := assert.New(t)
	defaultMixCode := "AutoPanMix(~maxClients).masterVolume_(1).selfVolume_(0.5).autopan_(true).panSlots_(1).hpf_(20).lpf_(20000).after({0.exit;}).connect.start;"
	assert.Equal(defaultMixCode, generateSuperColliderCode(defaultMixCode))
}

func TestGenerateSuperColliderCodeJSON(t *testing.T) {
	assert := assert.New(t)
	mixCode := `{"mixer":"SimpleMixer","masterOptions":[{"varName":"master","value":100}]}`
	scLangCode := `~preChain = SignalChain(~maxClients);

~postChain = SignalChain(~maxClients);

~mixer = SimpleMixer(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain).master_(100);

~mixer.connect.start;
`
	assert.Equal(scLangCode, generateSuperColliderCode(mixCode))
}
