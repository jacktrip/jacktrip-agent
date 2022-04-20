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
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPlaybackChannelNum(t *testing.T) {
	assert := assert.New(t)

	content := `
Generic Blue Microphones at usb-0000:01:00.0-1.1, high speed : USB Audio

Playback:
	Status: Stop
	Interface 2
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 4 OUT (ADAPTIVE)
	Rates: 44100
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR
	Interface 2
	Altset 2
	Format: S16_LE
	Channels: 2
	Endpoint: 4 OUT (ADAPTIVE)
	Rates: 48000
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR

Capture:
	Status: Stop
	Interface 1
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 1 IN (ASYNC)
	Rates: 44100
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR
	Interface 1
	Altset 2
	Format: S16_LE
	Channels: 2
	Endpoint: 1 IN (ASYNC)
	Rates: 48000
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR	
`
	result := getPlaybackChannelNum(strings.Split(content, "\n"), 48000)
	assert.Equal(2, result)
	result = getPlaybackChannelNum(strings.Split(content, "\n"), 44100)
	assert.Equal(2, result)
	result = getPlaybackChannelNum(strings.Split(content, "\n"), 96000)
	assert.Equal(-1, result)

	content = `
C-Media Electronics Inc. USB Audio Device at usb-0000:01:00.0-1.3, full speed : USB Audio

Playback:
	Status: Stop
	Interface 1
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 1 OUT (ADAPTIVE)
	Rates: 48000, 44100
	Bits: 16
	Channel map: FL FR

Capture:
	Status: Stop
	Interface 2
	Altset 1
	Format: S16_LE
	Channels: 1
	Endpoint: 2 IN (SYNC)
	Rates: 48000, 44100
	Bits: 16
	Channel map: MONO	
`
	result = getPlaybackChannelNum(strings.Split(content, "\n"), 48000)
	assert.Equal(2, result)
	result = getPlaybackChannelNum(strings.Split(content, "\n"), 44100)
	assert.Equal(2, result)
	result = getPlaybackChannelNum(strings.Split(content, "\n"), 96000)
	assert.Equal(-1, result)
}

func TestGetCaptureChannelNum(t *testing.T) {
	assert := assert.New(t)

	content := `
Generic Blue Microphones at usb-0000:01:00.0-1.1, high speed : USB Audio

Playback:
	Status: Stop
	Interface 2
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 4 OUT (ADAPTIVE)
	Rates: 44100
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR
	Interface 2
	Altset 2
	Format: S16_LE
	Channels: 2
	Endpoint: 4 OUT (ADAPTIVE)
	Rates: 48000
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR

Capture:
	Status: Stop
	Interface 1
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 1 IN (ASYNC)
	Rates: 44100
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR
	Interface 1
	Altset 2
	Format: S16_LE
	Channels: 2
	Endpoint: 1 IN (ASYNC)
	Rates: 48000
	Data packet interval: 1000 us
	Bits: 16
	Channel map: FL FR	
`
	result := getCaptureChannelNum(strings.Split(content, "\n"), 48000)
	assert.Equal(2, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 44100)
	assert.Equal(2, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 96000)
	assert.Equal(-1, result)

	content = `
C-Media Electronics Inc. USB Audio Device at usb-0000:01:00.0-1.3, full speed : USB Audio

Playback:
	Status: Stop
	Interface 1
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 1 OUT (ADAPTIVE)
	Rates: 48000, 44100
	Bits: 16
	Channel map: FL FR

Capture:
	Status: Stop
	Interface 2
	Altset 1
	Format: S16_LE
	Channels: 1
	Endpoint: 2 IN (SYNC)
	Rates: 48000, 44100
	Bits: 16
	Channel map: MONO	
`
	result = getCaptureChannelNum(strings.Split(content, "\n"), 48000)
	assert.Equal(1, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 44100)
	assert.Equal(1, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 96000)
	assert.Equal(-1, result)

	content = `
FMIC Fender Mustang Micro at usb-0000:01:00.0-1.1, full speed : USB Audio

Capture:
	Status: Stop
	Interface 1
	Altset 1
	Format: S16_LE
	Channels: 2
	Endpoint: 1 IN (ASYNC)
	Rates: 44100
	Bits: 16
	Channel map: FL FR
`
	result = getCaptureChannelNum(strings.Split(content, "\n"), 48000)
	assert.Equal(-1, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 44100)
	assert.Equal(2, result)
	result = getCaptureChannelNum(strings.Split(content, "\n"), 96000)
	assert.Equal(-1, result)
}

func TestWriteConfig(t *testing.T) {
	assert := assert.New(t)
	testFile, err := os.CreateTemp(os.TempDir(), "mixer_test")
	assert.NoError(err)
	t.Cleanup(func() {
		os.Remove(testFile.Name())
	})

	content := `
Lorem ipsum dolor sit amet, consectetur adipiscing elit.
Curabitur vestibulum lorem nulla, laoreet porttitor leo ultrices quis.
Maecenas a urna condimentum erat euismod venenatis.
`
	err = writeConfig(testFile.Name(), content)
	assert.NoError(err)

	data, _ := os.ReadFile(testFile.Name())
	assert.Equal(content, string(data))
}

func TestExtractNames(t *testing.T) {
	assert := assert.New(t)

	// Check 1 device
	content := `
**** List of PLAYBACK Hardware Devices ****
card 2: Microphone [USB2.0 Microphone], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
`
	names := extractNames(content)
	assert.Equal(1, len(names))
	assert.Equal("Microphone", names[0])

	// Check 2 devices
	content = `
**** List of PLAYBACK Hardware Devices ****
card 0: Device [USB Audio Device], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
card 1: Microphones [Blue Microphones], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
`
	names = extractNames(content)
	assert.Equal(2, len(names))
	assert.Equal("Device", names[0])
	assert.Equal("Microphones", names[1])

	// Check 3 devices
	content = `
**** List of PLAYBACK Hardware Devices ****
card 0: Device [USB Audio Device], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
card 1: Microphones [Blue Microphones], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
card 2: Microphone [USB2.0 Microphone], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
`
	names = extractNames(content)
	assert.Equal(3, len(names))
	assert.Equal("Device", names[0])
	assert.Equal("Microphones", names[1])
	assert.Equal("Microphone", names[2])
}

func TestExtractCardNum(t *testing.T) {
	assert := assert.New(t)

	// Check 1 device
	content := `
 2 [Microphone     ]: USB-Audio - USB2.0 Microphone
	Generic USB2.0 Microphone at usb-0000:01:00.0-1.2, high speed
`
	cardNum := extractCardNum(content)
	assert.Equal(1, len(cardNum))
	assert.Equal(2, cardNum["Microphone"])

	// Check 2 devices
	content = `
 1 [Device         ]: USB-Audio - USB Audio Device
	C-Media Electronics Inc. USB Audio Device at usb-0000:01:00.0-1.3, full speed
 2 [Microphone     ]: USB-Audio - USB2.0 Microphone
	Generic USB2.0 Microphone at usb-0000:01:00.0-1.2, high speed	
`
	cardNum = extractCardNum(content)
	assert.Equal(2, len(cardNum))
	assert.Equal(1, cardNum["Device"])
	assert.Equal(2, cardNum["Microphone"])

	// Check 3 devices
	content = `
 0 [Microphones    ]: USB-Audio - Blue Microphones
	Generic Blue Microphones at usb-0000:01:00.0-1.1, high speed
 1 [Device         ]: USB-Audio - USB Audio Device
	C-Media Electronics Inc. USB Audio Device at usb-0000:01:00.0-1.3, full speed
 2 [Microphone     ]: USB-Audio - USB2.0 Microphone
	Generic USB2.0 Microphone at usb-0000:01:00.0-1.2, high speed	
`
	cardNum = extractCardNum(content)
	assert.Equal(3, len(cardNum))
	assert.Equal(0, cardNum["Microphones"])
	assert.Equal(1, cardNum["Device"])
	assert.Equal(2, cardNum["Microphone"])
}
