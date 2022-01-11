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

func testSCOptionUnmarshal(assert *assert.Assertions, raw string, expected SCOption) {
	target := SCOption{}
	err := json.Unmarshal([]byte(raw), &target)
	assert.Nil(err)
	assert.Equal(expected.Name, target.Name)
	assert.Equal(expected.Value, target.Value)
	assert.Equal(len(expected.Names), len(target.Names))
	assert.Equal(expected.Names, target.Names)
	assert.Equal(len(expected.Values), len(target.Values))
	assert.Equal(expected.Values, target.Values)
}

func testSCLinkUnmarshal(assert *assert.Assertions, raw string, expected SCLink) {
	target := SCLink{}
	err := json.Unmarshal([]byte(raw), &target)
	assert.Nil(err)
	assert.Equal(expected.Name, target.Name)
	assert.Equal(len(expected.Options), len(target.Options))
	assert.Equal(expected.Options, target.Options)
}

func testSCSignalChainUnmarshal(assert *assert.Assertions, raw string, expected SCSignalChain) {
	target := SCSignalChain{}
	err := json.Unmarshal([]byte(raw), &target)
	assert.Nil(err)
	assert.Equal(len(expected), len(target))
	assert.Equal(expected, target)
}

func testSCConfigUnmarshal(assert *assert.Assertions, raw string, expected SCConfig) {
	target := SCConfig{}
	err := json.Unmarshal([]byte(raw), &target)
	assert.Nil(err)
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

func TestSCOption(t *testing.T) {
	assert := assert.New(t)
	testMarshal(assert, &SCOption{Name: "zero", Value: 0}, ".zero_(0)")
	testMarshal(assert, &SCOption{Name: "answer", Value: 42}, ".answer_(42)")
	testMarshal(assert, &SCOption{Name: "float", Value: 0.242}, ".float_(0.242000)")
	testMarshal(assert, &SCOption{Name: "negInt", Value: -21}, ".negInt_(-21)")
	testMarshal(assert, &SCOption{Name: "negFloat", Value: -4.2}, ".negFloat_(-4.200000)")
	testMarshal(assert, &SCOption{Name: "string", Value: "peppercorn"}, ".string_(\"peppercorn\")")

	testSCOptionUnmarshal(assert, `{"name": "zero", "value": 0}`, SCOption{Name: "zero", Value: float64(0)})
	testSCOptionUnmarshal(assert, `{"name": "answer", "value": 42}`, SCOption{Name: "answer", Value: float64(42)})
	testSCOptionUnmarshal(assert, `{"name": "float", "value": 0.242}`, SCOption{Name: "float", Value: float64(0.242)})
	testSCOptionUnmarshal(assert, `{"name": "negInt", "value": -21}`, SCOption{Name: "negInt", Value: float64(-21)})
	testSCOptionUnmarshal(assert, `{"name": "negFloat", "value": -4.2}`, SCOption{Name: "negFloat", Value: float64(-4.2)})
	testSCOptionUnmarshal(assert, `{"name": "string", "value": "peppercorn"}`, SCOption{Name: "string", Value: "peppercorn"})

	v := SCOption{
		Names:  []string{"zero", "answer", "float"},
		Values: []interface{}{float64(0), float64(42), float64(0.242)},
	}
	testMarshal(assert, &v, ".zero_(0).answer_(42).float_(0.242000)")
	testSCOptionUnmarshal(assert, `{"names": ["zero","answer","float"], "values": [0,42,0.242]}`, v)
}

func TestSCLink(t *testing.T) {
	assert := assert.New(t)
	link := SCLink{
		Name: "myLink",
		Options: []SCOption{
			{Name: "zero", Value: float64(0)},
			{Name: "negFloat", Value: float64(-4.2)},
		},
	}
	testMarshal(assert, &link, "myLink().zero_(0).negFloat_(-4.200000)")
	testSCLinkUnmarshal(assert, `{"name": "myLink", "options": [{"name": "zero", "value": 0}, {"name": "negFloat", "value": -4.2}]}`, link)
}

func TestSCSignalChain(t *testing.T) {
	assert := assert.New(t)
	chain := SCSignalChain{
		{
			Name: "myLink",
			Options: []SCOption{
				{Name: "float", Value: float64(0.242)},
			},
		},
		{
			Name: "yourLink",
			Options: []SCOption{
				{Name: "answer", Value: float64(42)},
			},
		},
	}
	testMarshal(assert, &chain, "SignalChain(~maxClients)\n.append(myLink().float_(0.242000))\n.append(yourLink().answer_(42))")
	testSCSignalChainUnmarshal(assert, `[{"name":"myLink","options":[{"name": "float", "value": 0.242}]},{"name":"yourLink","options":[{"name": "answer", "value": 42}]}]`, chain)
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
		MixerOptions: []SCOption{
			{Name: "master", Value: float64(100)},
		},
	}

	testMarshal(assert, &config, `~preChain = SignalChain(~maxClients);

~postChain = SignalChain(~maxClients);

~mixer = SimpleMixer(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain).master_(100);

~mixer.connect.start;
`)

	testSCConfigUnmarshal(assert, `{"mixer":"SimpleMixer","mixerOptions":[{"name":"master","value":100}]}`, config)
}

func TestSCConfigWithSignalChains(t *testing.T) {
	assert := assert.New(t)
	config := SCConfig{
		Mixer: "PersonalMixer",
		PreChain: SCSignalChain{
			{
				Name: "myLink",
				Options: []SCOption{
					{Name: "float", Value: float64(0.242)},
				},
			},
		},
		PostChain: SCSignalChain{
			{
				Name: "yourLink",
				Options: []SCOption{
					{Name: "answer", Value: float64(42)},
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
		"preChain" : [{"name":"myLink","options":[{"name": "float", "value": 0.242}]}],
		"postChain" : [{"name":"yourLink","options":[{"name": "answer", "value": 42}]}]
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
	mixCode := `{"mixer":"SimpleMixer","mixerOptions":[{"name":"master","value":100}]}`
	scLangCode := `~preChain = SignalChain(~maxClients);

~postChain = SignalChain(~maxClients);

~mixer = SimpleMixer(~maxClients).after({0.exit;}).preChain_(~preChain).postChain_(~postChain).master_(100);

~mixer.connect.start;
`
	assert.Equal(scLangCode, generateSuperColliderCode(mixCode))
}
