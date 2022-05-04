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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseALSAControls(t *testing.T) {
	assert := assert.New(t)
	var output string

	output = `
numid=6,iface=MIXER,name='DSP Program'
numid=27,iface=MIXER,name='ADC Left Capture Source'
numid=22,iface=MIXER,name='ADC Left Input'
numid=24,iface=MIXER,name='ADC Mic Bias'
numid=28,iface=MIXER,name='ADC Right Capture Source'
numid=23,iface=MIXER,name='ADC Right Input'
numid=21,iface=MIXER,name='ADC Capture Volume'
numid=3,iface=MIXER,name='Analogue Playback Boost Volume'
numid=2,iface=MIXER,name='Analogue Playback Volume'
numid=10,iface=MIXER,name='Auto Mute Mono Switch'
numid=11,iface=MIXER,name='Auto Mute Switch'
numid=8,iface=MIXER,name='Auto Mute Time Left'
numid=9,iface=MIXER,name='Auto Mute Time Right'
numid=7,iface=MIXER,name='Clock Missing Period'
numid=5,iface=MIXER,name='Deemphasis Switch'
numid=4,iface=MIXER,name='Digital Playback Switch'
numid=1,iface=MIXER,name='Digital Playback Volume'
numid=20,iface=MIXER,name='Max Overclock DAC'
numid=19,iface=MIXER,name='Max Overclock DSP'
numid=18,iface=MIXER,name='Max Overclock PLL'
numid=25,iface=MIXER,name='PGA Gain Left'
numid=26,iface=MIXER,name='PGA Gain Right'
numid=16,iface=MIXER,name='Volume Ramp Down Emergency Rate'
numid=17,iface=MIXER,name='Volume Ramp Down Emergency Step'
numid=12,iface=MIXER,name='Volume Ramp Down Rate'
numid=13,iface=MIXER,name='Volume Ramp Down Step'
numid=14,iface=MIXER,name='Volume Ramp Up Rate'
numid=15,iface=MIXER,name='Volume Ramp Up Step'
`
	result := parseALSAControls(output)
	assert.Equal(4, len(result))
	assert.Contains(result, "Digital Playback Volume")
	assert.Contains(result, "Analogue Playback Volume")
	assert.Contains(result, "ADC Capture Volume")
	assert.Contains(result, "Digital Playback Switch")

	output = `
numid=10,iface=CARD,name='Keep Interface'
numid=6,iface=MIXER,name='Mic Playback Switch'
numid=7,iface=MIXER,name='Mic Playback Volume'
numid=3,iface=MIXER,name='Mic Capture Switch'
numid=4,iface=MIXER,name='Mic Capture Volume'
numid=5,iface=MIXER,name='Extension Unit Switch'
numid=8,iface=MIXER,name='Speaker Playback Switch'
numid=9,iface=MIXER,name='Speaker Playback Volume'
numid=1,iface=PCM,name='Capture Channel Map'
numid=2,iface=PCM,name='Playback Channel Map'
`
	result = parseALSAControls(output)
	assert.Equal(6, len(result))
	assert.Contains(result, "Mic Playback Volume")
	assert.Contains(result, "Mic Playback Switch")
	assert.Contains(result, "Mic Capture Volume")
	assert.Contains(result, "Mic Capture Switch")
	assert.Contains(result, "Speaker Playback Volume")
	assert.Contains(result, "Speaker Playback Switch")

	output = `
numid=9,iface=CARD,name='Keep Interface'
numid=6,iface=MIXER,name='Headphone Playback Switch'
numid=7,iface=MIXER,name='Headphone Playback Volume'
numid=3,iface=MIXER,name='Mic Capture Switch'
numid=4,iface=MIXER,name='Mic Capture Volume'
numid=5,iface=MIXER,name='Extension Unit Switch'
numid=8,iface=MIXER,name='Sidetone Playback Switch'
numid=1,iface=PCM,name='Capture Channel Map'
numid=2,iface=PCM,name='Playback Channel Map'
`
	result = parseALSAControls(output)
	assert.Equal(5, len(result))
	assert.Contains(result, "Mic Capture Volume")
	assert.Contains(result, "Mic Capture Switch")
	assert.Contains(result, "Headphone Playback Volume")
	assert.Contains(result, "Headphone Playback Switch")
	assert.Contains(result, "Sidetone Playback Switch")

	output = `
numid=9,iface=CARD,name='Keep Interface'
numid=6,iface=MIXER,name='Headphone Playback Switch'
numid=7,iface=MIXER,name='Headphone Playback Volume'
numid=3,iface=MIXER,name='Mic Capture Switch'
numid=4,iface=MIXER,name='Capture Volume'
`
	result = parseALSAControls(output)
	assert.Equal(4, len(result))
	assert.Contains(result, "Capture Volume")
	assert.Contains(result, "Mic Capture Switch")
	assert.Contains(result, "Headphone Playback Volume")
	assert.Contains(result, "Headphone Playback Switch")
}
