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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// DeviceHeartbeatPath is a WSS API route used for bi-directional updates for a given device
	DeviceHeartbeatPath = "/devices/%s/heartbeat"

	// PathToAvahiServiceFile is the path to the avahi service file for jacktrip-agent
	PathToAvahiServiceFile = "/tmp/avahi/services/jacktrip-agent.service"

	// JackDeviceConfigTemplate is the template used to generate /tmp/default/jack file on raspberry pi devices
	JackDeviceConfigTemplate = "JACK_OPTS=-dalsa -d %s --rate %d --period %d\n"

	// JackDeviceNoAlsaConfigTemplate is the template used to generate /tmp/default/jack file on raspberry pi devices
	JackDeviceNoAlsaConfigTemplate = "JACK_OPTS=-d %s --rate %d --period %d\n"

	// JackTripDeviceConfigTemplate is the template used to generate /tmp/default/jacktrip file  on raspberry pi devices
	JackTripDeviceConfigTemplate = "JACKTRIP_OPTS=-t -z --udprt --receivechannels %d --sendchannels %d -C %s --peerport %d --bindport %d --clientname hubserver --remotename %s %s\n"

	// JamulusDeviceConfigTemplate is the template used to generate /tmp/default/jamulus file on raspberry pi devices
	JamulusDeviceConfigTemplate = "JAMULUS_OPTS=-n -i /tmp/jamulus.ini -c %s:%d\n"

	// DevicesRedirectURL is a template used to construct UI redirect URL for this device
	DevicesRedirectURL = "https://app.jacktrip.org/devices/%s?apiPrefix=%s&apiHash=%s"

	// PathToMACAddress is the path to ethernet device MAC address, via Linux kernel
	PathToMACAddress = "/sys/class/net/eth0/address"

	// PathToAsoundCards is the path to the ALSA card list
	PathToAsoundCards = "/proc/asound/cards"
)

var ac *AutoConnector
var soundDeviceName = ""
var soundDeviceType = ""
var lastDeviceStatus = "starting"
var currentDeviceConfig client.AgentConfig

// runOnDevice is used to run jacktrip-agent on a raspberry pi device
func runOnDevice(apiOrigin string) {
	log.Info("Running jacktrip-agent in device mode")

	// get sound device name and type
	soundDeviceName = getSoundDeviceName()
	soundDeviceType = getSoundDeviceType()
	log.Info("Detected sound device", "name", soundDeviceName, "type", soundDeviceType)

	// restore alsa card state, if saved state exists
	alsaStateFile := fmt.Sprintf("%s/asound.%s.state", AgentLibDir, soundDeviceType)
	if _, err := os.Stat(alsaStateFile); err == nil {
		log.Info("Restoring ALSA state", "file", alsaStateFile)
		cmd := exec.Command("/usr/sbin/alsactl", "restore", "--file", alsaStateFile)
		if err := cmd.Run(); err != nil {
			log.Error(err, "Unable to restore ALSA state", "file", alsaStateFile)
		}
	}

	// get mac and credentials
	mac := getMACAddress()
	credentials := getCredentials()

	// setup wait group for multiple routines
	var wg sync.WaitGroup

	// start HTTP server to redirect requests
	router := mux.NewRouter()
	router.HandleFunc("/ping", handlePingRequest).Methods("GET")
	router.PathPrefix("/info").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleDeviceInfoRequest(mac, credentials, w, r)
	})).Methods("GET")
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleDeviceRedirect(mac, credentials, w, r)
	})).Methods("GET")
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		OptionsGetOnly(w, r)
	})).Methods("OPTIONS")
	wg.Add(1)
	go runHTTPServer(&wg, router, ":80")

	// update avahi service config and restart daemon
	beat := client.DeviceHeartbeat{
		MAC:     mac,
		Version: getPatchVersion(),
		Type:    soundDeviceType,
		PingStats: client.PingStats{
			StatsUpdatedAt: time.Now(),
		},
	}
	updateAvahiServiceConfig(beat, credentials, lastDeviceStatus)

	// start sending heartbeats and updating agent configs
	wsm := WebSocketManager{
		ConfigChannel:    make(chan client.AgentConfig, 100),
		HeartbeatChannel: make(chan interface{}, 100),
		APIOrigin:        apiOrigin,
		Credentials:      credentials,
		HeartbeatPath:    DeviceHeartbeatPath,
	}
	wg.Add(1)
	go wsm.sendHeartbeatHandler(&wg)
	wg.Add(1)
	go wsm.recvConfigHandler(&wg)

	// Start JACK autoconnector
	ac = NewAutoConnector()
	wg.Add(1)
	go ac.Run(&wg)

	// Start device mixer
	dmm := DeviceMixingManager{
		CurrentCaptureDevices:  map[string]bool{},
		CurrentPlaybackDevices: map[string]bool{},
		DeviceStream0Mapping:   map[string][]string{},
		DeviceCardMapping:      map[string]int{},
		shutdown:               make(chan struct{}),
	}
	wg.Add(1)
	go dmm.Run(&wg)

	// start sending heartbeats and updating agent configs
	wg.Add(1)
	go sendDeviceHeartbeats(&wg, &beat, &wsm, &dmm)

	// Start a config handler to update config changes
	wg.Add(1)
	go deviceConfigUpdateHandler(&wg, &beat, &wsm, &dmm)

	// wait for everything to complete
	wg.Wait()
}

// deviceConfigUpdateHandler receives and processes device config updates
func deviceConfigUpdateHandler(wg *sync.WaitGroup, beat *client.DeviceHeartbeat, wsm *WebSocketManager, dmm *DeviceMixingManager) {
	defer wg.Done()
	log.Info("Starting deviceConfigUpdateHandler")
	for {
		newDeviceConfig, ok := <-wsm.ConfigChannel
		if !ok {
			log.Info("Config channel is closed")
			return
		}

		// just copy over parameters that we want to silently ignore
		currentDeviceConfig.Broadcast = newDeviceConfig.Broadcast
		currentDeviceConfig.ExpiresAt = newDeviceConfig.ExpiresAt

		if newDeviceConfig != currentDeviceConfig {
			// remove secrets before logging
			sanitizedDeviceConfig := newDeviceConfig
			sanitizedDeviceConfig.AuthToken = strings.Repeat("X", len(newDeviceConfig.AuthToken))
			log.Info("Config updated", "value", sanitizedDeviceConfig)

			// Check if the new config indicates a disconnect from an audio server. If yes, kill the existing socket as well.
			if wsm.IsInitialized && (!bool(newDeviceConfig.Enabled) || newDeviceConfig.Host == "") {
				wsm.CloseConnection()
			}
			handleDeviceUpdate(beat, wsm.Credentials, newDeviceConfig, dmm)
		}
	}
}

// sendDeviceHeartbeats sends device heartbeat messages to the backend api, and receives config updates
func sendDeviceHeartbeats(wg *sync.WaitGroup, beat *client.DeviceHeartbeat, wsm *WebSocketManager, dmm *DeviceMixingManager) {
	defer wg.Done()
	log.Info("Sending device heartbeats")
	firstHeartbeat := true

	for {
		// reconcile device version to handle first-time startup where patch files may be missing
		if beat.Version == "" {
			beat.Version = getPatchVersion()
		}

		if currentDeviceConfig.Enabled && currentDeviceConfig.Host != "" {
			// device is connected to an audio server

			// Measure connection latency to the audio server
			MeasurePingStats(beat, wsm.APIOrigin, currentDeviceConfig.Host, currentDeviceConfig.AuthToken) // blocks for 5 seconds instead of time sleep

			// Initialize a socket connection (do nothing if already connected)
			err := wsm.InitConnection(wg, beat.MAC)
			if err == nil {
				// send heartbeat to channel, for delivery over websocket
				wsm.HeartbeatChannel <- *beat
				continue
			}

			// fallback to sending heartbeat to HTTP endpoint if there is an error with websocket
			log.Error(err, "Falling back to HTTP heartbeats")

		} else {
			// device is not connected to an audio server

			if firstHeartbeat {
				// don't sleep before sending first heartbeat
				firstHeartbeat = false
			} else {
				// sleep for heartbeat interval
				time.Sleep(HeartbeatInterval * time.Second)
			}

			// reset ping stats to be empty, with current timestamp
			beat.PingStats = client.PingStats{StatsUpdatedAt: time.Now()}
		}

		// there is no websocket connection to the api server, so send heartbeat to HTTP endpoint

		// send http heartbeat message to api server
		newDeviceConfig, err := sendHTTPHeartbeat(*beat, wsm.Credentials, wsm.APIOrigin)
		if err != nil {
			updateDeviceStatus(*beat, wsm.Credentials, "error")
			panic(err)
		}

		// send device config received from response to channel
		wsm.ConfigChannel <- newDeviceConfig
	}
}

// handleDeviceUpdate handles updates to device configuratiosn
func handleDeviceUpdate(beat *client.DeviceHeartbeat, credentials client.AgentCredentials, config client.AgentConfig, dmm *DeviceMixingManager) {
	// update current config sooner, so that other goroutines will have the most up-to-date version
	lastDeviceConfig := currentDeviceConfig
	currentDeviceConfig = config

	// update ALSA card settings
	if config.ALSAConfig != lastDeviceConfig.ALSAConfig {
		updateALSASettings(config)
	}

	// check if ALSA card settings was the only change
	lastDeviceConfig.ALSAConfig = config.ALSAConfig
	if config != lastDeviceConfig {
		// more changes required -> reset everything

		// update managed config files
		updateServiceConfigs(config, strings.Replace(beat.MAC, ":", "", -1))

		// shutdown or restart managed services
		ac.TeardownClient()
		restartAllServices(config)
		if config.Enabled && config.Host != "" && config.Type != "" {
			ac.SetupClient()
			dmm.Reset()
		}
	}

	// update device status in avahi service config, if necessary
	if config.Enabled {
		updateDeviceStatus(*beat, credentials, "connected")
	} else {
		updateDeviceStatus(*beat, credentials, "not connected")
	}
}

// getMACAddress retrieves ethernet device MAC address, via Linux kernel
func getMACAddress() string {
	macBytes, err := ioutil.ReadFile(PathToMACAddress)
	if err != nil {
		log.Error(err, "Unable to retrieve MAC address")
		panic(err)
	}

	// trip whitespace and convert to lowercase
	mac := strings.TrimSpace(string(macBytes))
	mac = strings.ToLower(mac)

	log.Info("Retrieved MAC address", "mac", mac)
	return mac
}

// getPatchVersion retrieves patch version for the device
func getPatchVersion() string {
	rawBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/patch", AgentConfigDir))
	if err != nil {
		return ""
	}

	// trim whitespace
	patchVersion := strings.TrimSpace(string(rawBytes))

	log.Info("Retrieved patch version", "version", patchVersion)
	return patchVersion
}

// getSoundDeviceName retrieves alsa name for the sound device
func getSoundDeviceName() string {
	rawBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/devicename", AgentConfigDir))
	if err != nil {
		log.Error(err, "Unable to retrieve name of sound device")
		panic(err)
	}
	name := strings.TrimSpace(string(rawBytes))
	if name == "dummy" {
		return name
	}
	return fmt.Sprintf("hw:%s", name)
}

// getSoundDeviceType retrieves alsa type for the sound device
func getSoundDeviceType() string {
	rawBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/devicetype", AgentConfigDir))
	if err != nil {
		log.Error(err, "Unable to retrieve type of sound device")
		panic(err)
	}
	return strings.TrimSpace(string(rawBytes))
}

// updateALSASettings is used to update the settings for an ALSA sound card
func updateALSASettings(config client.AgentConfig) {
	switch soundDeviceType {
	case "snd_rpi_hifiberry_dacplusadc":
		fallthrough
	case "snd_rpi_hifiberry_dacplusadcpro":
		updateALSASettingsHiFiBerry(config)
	case "audioinjector-pi-soundcard":
		updateALSASettingsAudioInjector(config)
	case "USB Audio Device":
		updateALSASettingsUSBAudioDevice(config)
	case "USB PnP Sound Device":
		updateALSASettingsUSBPnPSoundDevice(config)
	default:
		log.Info("No ALSA alsa controls for sound device", "type", soundDeviceType)
	}
}

// updateALSASettings is used to update the settings for a HiFiBerry sound card
func updateALSASettingsHiFiBerry(config client.AgentConfig) {
	var v int
	amixerDevice := fmt.Sprintf("hw:%s", soundDeviceName)

	// ignore capture boost
	/*
		if config.CaptureBoost {
			v = 104
		} else {
			v = 0
		}
		cmd := exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='PGA Gain Left'", "--", fmt.Sprintf("%d", v))
		if err := cmd.Run(); err != nil {
			log.Error(err, "Unable to update 'PGA Gain Left'", "value", v)
		} else {
			log.Info("Updated 'PGA Gain Left'", "value", v)
		}
		cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='PGA Gain Right'", "--", fmt.Sprintf("%d", v))
		if err := cmd.Run(); err != nil {
			log.Error(err, "Unable to update 'PGA Gain Right'", "value", v)
		} else {
			log.Info("Updated 'PGA Gain Right'", "value", v)
		}
	*/

	// set capture volume
	// note: 'PGA Gain Left' and 'PGA Gain Right' appear to map directly to 'ADC Capture Volume' left & right
	v = int(config.CaptureVolume * 104 / 100)
	cmd := exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='ADC Capture Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'ADC Capture Volume'", "value", v)
	} else {
		log.Info("Updated 'ADC Capture Volume'", "value", v)
	}

	// set playback boost
	if config.PlaybackBoost {
		v = 1
	} else {
		v = 0
	}
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Analogue Playback Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Analogue Playback Volume'", "value", v)
	} else {
		log.Info("Updated 'Analogue Playback Volume'", "value", v)
	}

	// set playback volume
	v = int(config.PlaybackVolume * 207 / 100)
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Digital Playback Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Digital Playback Volume' to %d: %s", "value", v)
	} else {
		log.Info("Updated 'Digital Playback Volume'", "value", v)
	}
}

// updateALSASettingsAudioInjector is used to update the settings for a Audio Injector Stereo sound card
func updateALSASettingsAudioInjector(config client.AgentConfig) {
	var v int
	amixerDevice := fmt.Sprintf("hw:%s", soundDeviceName)

	// enable built in mic with boost, if set
	if config.CaptureBoost {
		v = 1
	} else {
		v = 0
	}
	cmd := exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Mic Boost Volume'", "--", fmt.Sprintf("%d", v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Mic Boost Volume'", "value", v)
	} else {
		log.Info("Updated 'Mic Boost Volume'", "value", v)
	}
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Input Mux'", "--", fmt.Sprintf("%d", v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Input Mux'", "value", v)
	} else {
		log.Info("Updated 'Input Mux'", "value", v)
	}

	// set capture volume
	v = int(config.CaptureVolume * 31 / 100)
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Capture Volume'", "--", fmt.Sprintf("%d", v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Capture Volume'", "value", v)
	} else {
		log.Info("Updated 'Capture Volume'", "value", v)
	}

	// set playback volume
	v = int(config.PlaybackVolume * 127 / 100)
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Master Playback Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Master Playback Volume' to %d: %s", "value", v)
	} else {
		log.Info("Updated 'Master Playback Volume'", "value", v)
	}
}

// updateALSASettingsUSBAudioDevice is used to update the settings for a USB sound card
func updateALSASettingsUSBAudioDevice(config client.AgentConfig) {
	var v int
	amixerDevice := fmt.Sprintf("hw:%s", soundDeviceName)

	// set capture volume
	v = int(config.CaptureVolume * 35 / 100)
	cmd := exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Mic Capture Volume'", "--", fmt.Sprintf("%d", v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Mic Capture Volume'", "value", v)
	} else {
		log.Info("Updated 'Mic Capture Volume'", "value", v)
	}

	// set playback volume
	v = int(config.PlaybackVolume * 37 / 100)
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Speaker Playback Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Speaker Playback Volume' to %d: %s", "value", v)
	} else {
		log.Info("Updated 'Speaker Playback Volume'", "value", v)
	}
}

// updateALSASettingsUSBPnPSoundDevice is used to update the settings for a USB sound card
func updateALSASettingsUSBPnPSoundDevice(config client.AgentConfig) {
	var v int
	amixerDevice := fmt.Sprintf("hw:%s", soundDeviceName)

	// set capture volume
	v = int(config.CaptureVolume * 16 / 100)
	cmd := exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Mic Capture Volume'", "--", fmt.Sprintf("%d", v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Mic Capture Volume'", "value", v)
	} else {
		log.Info("Updated 'Mic Capture Volume'", "value", v)
	}

	// set playback volume
	v = int(config.PlaybackVolume * 151 / 100)
	cmd = exec.Command("/usr/bin/amixer", "-D", amixerDevice, "cset", "name='Speaker Playback Volume'", "--", fmt.Sprintf("%d,%d", v, v))
	if err := cmd.Run(); err != nil {
		log.Error(err, "Unable to update 'Speaker Playback Volume' to %d: %s", "value", v)
	} else {
		log.Info("Updated 'Speaker Playback Volume'", "value", v)
	}
}

// updateAvahiServiceConfig generates a new /etc/avahi/services/jacktrip-agent.service file
func updateAvahiServiceConfig(beat client.DeviceHeartbeat, credentials client.AgentCredentials, status string) {
	// ensure config directory exists
	err := os.MkdirAll("/tmp/avahi/services", 0755)
	if err != nil {
		log.Error(err, "Failed to create directory", "path", "/tmp/avahi/services")
		return
	}

	apiHash := client.GetAPIHash(credentials.APISecret)
	avahiServiceConfig := fmt.Sprintf(`<?xml version="1.0" standalone='no'?><!--*-nxml-*-->
<!DOCTYPE service-group SYSTEM "avahi-service.dtd">
<service-group>
	<name replace-wildcards="yes">JackTrip Agent on %%h</name>
	<service>
		<type>_http._tcp</type>
		<port>80</port>
		<txt-record value-format="text">status=%s</txt-record>
		<txt-record value-format="text">version=%s</txt-record>
		<txt-record value-format="text">mac=%s</txt-record>
		<txt-record value-format="text">apiHash=%s</txt-record>
	</service>
</service-group>
`, status, beat.Version, beat.MAC, apiHash)

	err = ioutil.WriteFile(PathToAvahiServiceFile, []byte(avahiServiceConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save avahi service config", "path", PathToAvahiServiceFile)
		return
	}
	log.Info(fmt.Sprintf("Updated Avahi service status to %s", status))
}

// updateDeviceStatus updates the device status, including avahi config, if it has changed
func updateDeviceStatus(beat client.DeviceHeartbeat, credentials client.AgentCredentials, status string) {
	log.Info(fmt.Sprintf("Updated device status to %s", status))
	if lastDeviceStatus != status {
		updateAvahiServiceConfig(beat, credentials, status)
		lastDeviceStatus = status
	}
}

// handleDeviceInfoRequest returns information about a device
func handleDeviceInfoRequest(mac string, credentials client.AgentCredentials, w http.ResponseWriter, r *http.Request) {
	apiHash := client.GetAPIHash(credentials.APISecret)
	deviceInfo := struct {
		APIPrefix string `json:"apiPrefix"`
		APIHash   string `json:"apiHash"`
		MAC       string `json:"mac"`
	}{
		APIPrefix: credentials.APIPrefix,
		APIHash:   apiHash,
		MAC:       mac,
	}
	RespondJSON(w, http.StatusOK, deviceInfo)
}

// handleDeviceRedirect redirects all requests to devices in jacktrip web application
func handleDeviceRedirect(mac string, credentials client.AgentCredentials, w http.ResponseWriter, r *http.Request) {
	apiHash := client.GetAPIHash(credentials.APISecret)
	w.Header().Set("Location", fmt.Sprintf(DevicesRedirectURL, mac, credentials.APIPrefix, apiHash))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusSeeOther)
}
