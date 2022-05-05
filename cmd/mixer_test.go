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

func TestFindNewDevices(t *testing.T) {
	assert := assert.New(t)

	// Case for no new devices
	oldDevices := map[string]bool{"one": true}
	activeDevices := map[string]bool{"one": true}
	result := findNewDevices(oldDevices, activeDevices)
	assert.Equal(0, len(result))

	// Case for 1 new device
	oldDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"one": true, "two": true}
	result = findNewDevices(oldDevices, activeDevices)
	assert.Equal(1, len(result))
	assert.Equal("two", result[0])

	// Case for 1 new device
	oldDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"three": true}
	result = findNewDevices(oldDevices, activeDevices)
	assert.Equal(1, len(result))
	assert.Equal("three", result[0])

	// Case for multiple new devices
	oldDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"two": true, "three": true}
	result = findNewDevices(oldDevices, activeDevices)
	assert.Equal(2, len(result))
	assert.Contains(result, "two")
	assert.Contains(result, "three")

	// Case for multiple new devices
	oldDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"one": true, "three": true, "four": true, "five": true}
	result = findNewDevices(oldDevices, activeDevices)
	assert.Equal(3, len(result))
	assert.Contains(result, "three")
	assert.Contains(result, "four")
	assert.Contains(result, "five")
}

func TestRemoveInactiveDevices(t *testing.T) {
	// NOTE: This test spews a bunch of dbus error logs that we're ignoring
	assert := assert.New(t)

	// Case for no active devices
	foundDevices := map[string]bool{"one": true}
	activeDevices := map[string]bool{}
	removeInactiveDevices(foundDevices, activeDevices, ZitaCapture)
	assert.Equal(0, len(foundDevices))

	// Case for 1 active device
	foundDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"one": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaCapture)
	assert.Equal(1, len(foundDevices))
	assert.Contains(foundDevices, "one")

	// Case for 1 active-but-not-found device
	foundDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"two": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaCapture)
	assert.Equal(0, len(foundDevices))

	// Case for multiple active devices
	foundDevices = map[string]bool{"one": true}
	activeDevices = map[string]bool{"one": true, "two": true, "three": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaPlayback)
	assert.Equal(1, len(foundDevices))
	assert.Contains(foundDevices, "one")

	// Case for multiple active devices
	foundDevices = map[string]bool{"one": true, "three": true}
	activeDevices = map[string]bool{"one": true, "two": true, "three": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaCapture)
	assert.Equal(2, len(foundDevices))
	assert.Contains(foundDevices, "one")
	assert.Contains(foundDevices, "three")

	// Case for multiple disjoint active devices
	foundDevices = map[string]bool{"one": true, "three": true}
	activeDevices = map[string]bool{"two": true, "four": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaPlayback)
	assert.Equal(0, len(foundDevices))

	// Case for multiple active devices that overlap with found devices
	foundDevices = map[string]bool{"one": true, "three": true}
	activeDevices = map[string]bool{"two": true, "three": true, "four": true}
	removeInactiveDevices(foundDevices, activeDevices, ZitaCapture)
	assert.Equal(1, len(foundDevices))
	assert.Contains(foundDevices, "three")

	// Case for multiple active devices that overlap with found devices
	foundDevices = map[string]bool{"one": true, "two": true, "three": true, "five": true}
	activeDevices = map[string]bool{
		"one":  true,
		"two":  true,
		"four": true,
		"five": true,
		"six":  true,
	}
	removeInactiveDevices(foundDevices, activeDevices, ZitaPlayback)
	assert.Equal(3, len(foundDevices))
	assert.Contains(foundDevices, "one")
	assert.Contains(foundDevices, "two")
	assert.Contains(foundDevices, "five")
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

func TestFindBestSampleRateAndChannel(t *testing.T) {
	assert := assert.New(t)

	// Case for empty map
	rateToChannelsMap := map[int]int{}
	rate, channels := findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(0, rate)
	assert.Equal(-1, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(0, rate)
	assert.Equal(-1, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(0, rate)
	assert.Equal(-1, channels)

	// Case for only 48000::1 in map
	rateToChannelsMap = map[int]int{48000: 1}
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(48000, rate)
	assert.Equal(1, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(48000, rate)
	assert.Equal(1, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(48000, rate)
	assert.Equal(1, channels)

	// Case for only 48000::2 in map
	rateToChannelsMap = map[int]int{48000: 2}
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(48000, rate)
	assert.Equal(2, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(48000, rate)
	assert.Equal(2, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(48000, rate)
	assert.Equal(2, channels)

	// Case for only 44100::3 in map
	rateToChannelsMap = map[int]int{44100: 3}
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(44100, rate)
	assert.Equal(3, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(44100, rate)
	assert.Equal(3, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(44100, rate)
	assert.Equal(3, channels)

	// Case for both 48000 and 44100 in map
	rateToChannelsMap = map[int]int{48000: 1, 44100: 3}
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(44100, rate)
	assert.Equal(3, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(48000, rate)
	assert.Equal(1, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(48000, rate)
	assert.Equal(1, channels)

	// Case for multiple items in map
	rateToChannelsMap = map[int]int{44100: 5, 48000: 2, 96000: 1}
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 44100)
	assert.Equal(44100, rate)
	assert.Equal(5, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 48000)
	assert.Equal(48000, rate)
	assert.Equal(2, channels)
	rate, channels = findBestSampleRateAndChannel(rateToChannelsMap, 96000)
	assert.Equal(96000, rate)
	assert.Equal(1, channels)
}

func TestGetSampleRateToChannelMap(t *testing.T) {
	assert := assert.New(t)
	var zMode ZitaMode
	var content string

	// Tests for mode == ZitaPlayback
	zMode = ZitaPlayback

	content = `
Generic Blue Microphones at usb-0000:01:00.0-1.3, high speed : USB Audio

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
	result := getSampleRateToChannelMap(strings.Split(content, "\n"), zMode)
	assert.Equal(2, len(result))
	assert.Equal(2, result[44100])
	assert.Equal(2, result[48000])

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
	result = getSampleRateToChannelMap(strings.Split(content, "\n"), zMode)
	assert.Equal(2, len(result))
	assert.Equal(2, result[44100])
	assert.Equal(2, result[48000])

	// Tests for mode == ZitaCapture
	zMode = ZitaCapture
	content = `
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
	result = getSampleRateToChannelMap(strings.Split(content, "\n"), zMode)
	assert.Equal(2, len(result))
	assert.Equal(2, result[44100])
	assert.Equal(2, result[48000])

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
	result = getSampleRateToChannelMap(strings.Split(content, "\n"), zMode)
	assert.Equal(2, len(result))
	assert.Equal(1, result[44100])
	assert.Equal(1, result[48000])

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
	result = getSampleRateToChannelMap(strings.Split(content, "\n"), zMode)
	assert.Equal(1, len(result))
	assert.Equal(2, result[44100])
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
	assert.Contains(names, "Microphone")

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
	assert.Contains(names, "Device")
	assert.Contains(names, "Microphones")

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
	assert.Contains(names, "Device")
	assert.Contains(names, "Microphones")
	assert.Contains(names, "Microphone")

	// Check devices with higher card numbers
	content = `
**** List of PLAYBACK Hardware Devices ****
card 10: Device [USB Audio Device], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
card 20: Microphones [Blue Microphones], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
card 31: Microphone [USB2.0 Microphone], device 0: USB Audio [USB Audio]
	Subdevices: 1/1
	Subdevice #0: subdevice #0
`
	names = extractNames(content)
	assert.Equal(3, len(names))
	assert.Contains(names, "Device")
	assert.Contains(names, "Microphones")
	assert.Contains(names, "Microphone")
}

func TestExtractCardNum(t *testing.T) {
	assert := assert.New(t)

	// Check hifiberry card
	content := `
 0 [sndrpihifiberry]: HifiberryDacpAd - snd_rpi_hifiberry_dacplusadcpro
	snd_rpi_hifiberry_dacplusadcpro
 1 [Microphones    ]: USB-Audio - Blue Microphones
	Generic Blue Microphones at usb-0000:01:00.0-1.3, high speed
`
	cardNum := extractCardNum(content)
	assert.Equal(2, len(cardNum))
	assert.Equal(0, cardNum["sndrpihifiberry"])
	assert.Equal(1, cardNum["Microphones"])

	// Check 1 device
	content = `
 2 [Microphone     ]: USB-Audio - USB2.0 Microphone
	Generic USB2.0 Microphone at usb-0000:01:00.0-1.2, high speed
`
	cardNum = extractCardNum(content)
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

	// Check devices with higher card numbers
	content = `
 31 [Microphones    ]: USB-Audio - Blue Microphones
	Generic Blue Microphones at usb-0000:01:00.0-1.1, high speed
 101 [Device         ]: USB-Audio - USB Audio Device
	C-Media Electronics Inc. USB Audio Device at usb-0000:01:00.0-1.3, full speed
 20 [Microphone     ]: USB-Audio - USB2.0 Microphone
	Generic USB2.0 Microphone at usb-0000:01:00.0-1.2, high speed	
`
	cardNum = extractCardNum(content)
	assert.Equal(3, len(cardNum))
	assert.Equal(31, cardNum["Microphones"])
	assert.Equal(101, cardNum["Device"])
	assert.Equal(20, cardNum["Microphone"])
}

func TestParseSampleRates(t *testing.T) {
	assert := assert.New(t)

	line := "Rates: 44100"
	result := parseSampleRates(line)
	assert.Equal(1, len(result))
	assert.Contains(result, 44100)

	line = "Rates: 48000, 44100"
	result = parseSampleRates(line)
	assert.Equal(2, len(result))
	assert.Contains(result, 48000)
	assert.Contains(result, 44100)

	line = "Rates: 192000, 96000"
	result = parseSampleRates(line)
	assert.Equal(2, len(result))
	assert.Contains(result, 192000)
	assert.Contains(result, 96000)

	line = "Rates: 192000"
	result = parseSampleRates(line)
	assert.Equal(1, len(result))
	assert.Contains(result, 192000)

	line = "Rates: "
	result = parseSampleRates(line)
	assert.Equal(0, len(result))

	line = "Rates: blahblah"
	result = parseSampleRates(line)
	assert.Equal(0, len(result))
}
