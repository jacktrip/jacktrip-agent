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
	// DeviceStatusPath is a WSS API route used for bi-directional updates for a given device
	DeviceStatusPath = "/devices/%s/status"

	// PathToAvahiServiceFile is the path to the avahi service file for jacktrip-agent
	PathToAvahiServiceFile = "/tmp/avahi/services/jacktrip-agent.service"

	// JackDeviceConfigTemplate is the template used to generate /tmp/default/jack file on raspberry pi devices
	JackDeviceConfigTemplate = "JACK_OPTS=-dalsa -dhw:%s --rate %d --period %d\n"

	// JackTripDeviceConfigTemplate is the template used to generate /tmp/default/jacktrip file  on raspberry pi devices
	JackTripDeviceConfigTemplate = "JACKTRIP_OPTS=-t -z --udprt -n %d -C %s --peerport %d --bindport %d --clientname hubserver --remotename %s %s\n"

	// JamulusDeviceConfigTemplate is the template used to generate /tmp/default/jamulus file on raspberry pi devices
	JamulusDeviceConfigTemplate = "JAMULUS_OPTS=-n -i /tmp/jamulus.ini -c %s:%d\n"

	// DevicesRedirectURL is a template used to construct UI redirect URL for this device
	DevicesRedirectURL = "https://app.jacktrip.org/devices/%s?apiPrefix=%s&apiHash=%s"

	// PathToMACAddress is the path to ethernet device MAC address, via Linux kernel
	PathToMACAddress = "/sys/class/net/eth0/address"

	// PathToAsoundCards is the path to the ALSA card list
	PathToAsoundCards = "/proc/asound/cards"
)

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
	ping := client.AgentPing{
		AgentCredentials: credentials,
		MAC:              mac,
		Version:          getPatchVersion(),
		Type:             soundDeviceType,
	}
	updateAvahiServiceConfig(&ping, lastDeviceStatus)

	// start ping server to send pings and update agent config
	wg.Add(1)
	wsm := WebSocketManager{ConfigChannel: make(chan client.AgentConfig, 100), PingStatsChannel: make(chan client.PingStats, 100)}
	go runDeviceConfigPinger(&wg, &ping, &wsm, apiOrigin)

	// Start a config handler to update config changes
	wg.Add(1)
	go runConfigUpdateHandler(&wg, &ping, &wsm)

	// wait for everything to complete
	wg.Wait()
}

func runConfigUpdateHandler(wg *sync.WaitGroup, ping *client.AgentPing, wsm *WebSocketManager) {
	defer wg.Done()
	log.Info("Starting getDeviceConfigHandler")
	for {
		select {
		case newDeviceConfig, ok := <-wsm.ConfigChannel:
			if !ok {
				log.Info("Config channel is closed")
				return
			}
			if newDeviceConfig != currentDeviceConfig {
				// Check if the new config indicates a disconnect from an audio server. If yes, kill the existing socket as well.
				if wsm.IsInitialized == true && (newDeviceConfig.Enabled == false || newDeviceConfig.Host == "") {
					wsm.CloseConnection()
				}
				handleDeviceUpdate(ping, newDeviceConfig)
			}
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runDeviceConfigPinger sends pings to service and manages config updates
func runDeviceConfigPinger(wg *sync.WaitGroup, ping *client.AgentPing, wsm *WebSocketManager, apiOrigin string) {
	defer wg.Done()
	log.Info("Starting agent ping server")

	for {
		// If the device isn't connected to an audio server, there is no socket connection to the api server so use a regular ping endpoint.
		if wsm.IsInitialized == false || currentDeviceConfig.Host == "" {
			newDeviceConfig, err := sendPing(*ping, apiOrigin)
			if err != nil {
				updateDeviceStatus(ping, "error")
				panic(err)
			}

			if newDeviceConfig != currentDeviceConfig {
				wsm.ConfigChannel <- newDeviceConfig
			}
		}

		if currentDeviceConfig.Enabled == true && currentDeviceConfig.Host != "" {
			// Initialize a socket connection
			err := wsm.InitConnection(wg, ping, apiOrigin, DeviceStatusPath)
			if err != nil {
				updateDeviceStatus(ping, "error")
			}

			// Measure connection latency to the audio server
			MeasurePingStats(ping, apiOrigin, currentDeviceConfig.Host, HTTPServerPort) // blocks for 5 seconds instead of time sleep
		} else {
			time.Sleep(AgentPingInterval * time.Second)
		}

		if currentDeviceConfig.Enabled == true && currentDeviceConfig.Host != "" {
			wsm.PingStatsChannel <- ping.PingStats
		}
	}
}

// handleDeviceUpdate handles updates to device configuratiosn
func handleDeviceUpdate(ping *client.AgentPing, config client.AgentConfig) {
	// update ALSA card settings
	if config.ALSAConfig != currentDeviceConfig.ALSAConfig {
		updateALSASettings(config)
	}

	// check if ALSA card settings was the only change
	currentDeviceConfig.ALSAConfig = config.ALSAConfig
	if config != currentDeviceConfig {
		// more changes required -> reset everything

		// update managed config files
		updateServiceConfigs(config, strings.Replace(ping.MAC, ":", "", -1), false)

		// shutdown or restart managed services
		restartAllServices(config, false)
	}

	currentDeviceConfig = config

	// update device status in avahi service config, if necessary
	if config.Enabled {
		updateDeviceStatus(ping, "connected")
	} else {
		updateDeviceStatus(ping, "not connected")
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
	return strings.TrimSpace(string(rawBytes))
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
func updateAvahiServiceConfig(ping *client.AgentPing, status string) {
	// ensure config directory exists
	err := os.MkdirAll("/tmp/avahi/services", 0755)
	if err != nil {
		log.Error(err, "Failed to create directory", "path", "/tmp/avahi/services")
		return
	}

	apiHash := client.GetAPIHash(ping.APISecret)
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
`, status, ping.Version, ping.MAC, apiHash)

	err = ioutil.WriteFile(PathToAvahiServiceFile, []byte(avahiServiceConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save avahi service config", "path", PathToAvahiServiceFile)
		return
	}
	log.Info(fmt.Sprintf("Updated Avahi service status to %s", status))
}

// updateDeviceStatus updates the device status, including avahi config, if it has changed
func updateDeviceStatus(ping *client.AgentPing, status string) {
	log.Info(fmt.Sprintf("Updated device status to %s", status))
	if lastDeviceStatus != status {
		updateAvahiServiceConfig(ping, status)
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
