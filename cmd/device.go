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
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
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
	JackDeviceConfigTemplate = "JACK_OPTS=-d %s --rate %d --period %d\n"

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

	// ALSAInputSourceToken is a regex pattern to determine if an ALSA control is used for input: https://www.kernel.org/doc/html/latest/sound/designs/control-names.html
	ALSAInputSourceToken = `Mic|ADC`
)

var ac *AutoConnector
var soundDeviceName = ""
var soundDeviceType = ""
var lastDeviceStatus = "starting"
var currentDeviceConfig client.DeviceAgentConfig

// runOnDevice is used to run jacktrip-agent on a raspberry pi device
func runOnDevice(apiOrigin string) {
	log.Info("Running jacktrip-agent in device mode")

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

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

	// setup cancellation context and wait group for multiple routines
	ctx, cancel := context.WithCancel(context.Background())
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
	server := runHTTPServer(&wg, router, ":80")

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
		ConfigChannel:    make(chan client.DeviceAgentConfig, 100),
		HeartbeatChannel: make(chan interface{}, 100),
		APIOrigin:        apiOrigin,
		Credentials:      credentials,
		HeartbeatPath:    DeviceHeartbeatPath,
	}
	wg.Add(1)
	go wsm.sendHeartbeatHandler(ctx, &wg)

	wg.Add(1)
	go wsm.recvConfigHandler(ctx, &wg)

	// Start JACK autoconnector
	ac = NewAutoConnector()
	wg.Add(1)
	go ac.Run(ctx, &wg)

	// Start device mixer
	dmm := DeviceMixingManager{
		CurrentCaptureDevices:  map[string]bool{},
		CurrentPlaybackDevices: map[string]bool{},
		DeviceStream0Mapping:   map[string][]string{},
		DeviceCardMapping:      map[string]int{},
	}
	wg.Add(1)
	go dmm.Run(ctx, &wg)

	// start sending heartbeats and updating agent configs
	wg.Add(1)
	go sendDeviceHeartbeats(ctx, &wg, &beat, &wsm, &dmm)

	// Start a config handler to update config changes
	wg.Add(1)
	go deviceConfigUpdateHandler(ctx, &wg, &beat, &wsm, &dmm)

	// Wait for process exit signal, then terminate all goroutines
	<-exit
	shutdownHTTPServer(server)
	if wsm.IsInitialized {
		wsm.CloseConnection()
	}
	cancel()

	// wait for everything to complete
	wg.Wait()
}

// deviceConfigUpdateHandler receives and processes device config updates
func deviceConfigUpdateHandler(ctx context.Context, wg *sync.WaitGroup, beat *client.DeviceHeartbeat, wsm *WebSocketManager, dmm *DeviceMixingManager) {
	defer wg.Done()
	log.Info("Starting deviceConfigUpdateHandler")
	firstConfig := true

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping deviceConfigUpdateHandler")
			return
		case newDeviceConfig := <-wsm.ConfigChannel:
			if firstConfig || newDeviceConfig != currentDeviceConfig {
				// remove secrets before logging
				sanitizedDeviceConfig := newDeviceConfig
				sanitizedDeviceConfig.AuthToken = strings.Repeat("X", len(newDeviceConfig.AuthToken))
				log.Info("Config updated", "value", sanitizedDeviceConfig)

				// Check if the new config indicates a disconnect from an audio server. If yes, kill the existing socket as well.
				if wsm.IsInitialized && (!bool(newDeviceConfig.Enabled) || newDeviceConfig.Host == "") {
					wsm.CloseConnection()
				}
				// Force full device update on the first config received
				handleDeviceUpdate(beat, wsm.Credentials, newDeviceConfig, dmm, firstConfig)
				firstConfig = false
			}
		}
	}
}

// sendDeviceHeartbeats sends device heartbeat messages to the backend api, and receives config updates
func sendDeviceHeartbeats(ctx context.Context, wg *sync.WaitGroup, beat *client.DeviceHeartbeat, wsm *WebSocketManager, dmm *DeviceMixingManager) {
	defer wg.Done()
	log.Info("Starting sendDeviceHeartbeats")
	firstHeartbeat := true

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping sendDeviceHeartbeats")
			return
		default:
		}

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
func handleDeviceUpdate(beat *client.DeviceHeartbeat, credentials client.AgentCredentials, config client.DeviceAgentConfig, dmm *DeviceMixingManager, force bool) {
	// update current config sooner, so that other goroutines will have the most up-to-date version
	lastDeviceConfig := currentDeviceConfig
	currentDeviceConfig = config

	// update ALSA card settings
	if force || config.ALSAConfig != lastDeviceConfig.ALSAConfig {
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
		dmm.Reset()
		restartAllServices(config)
		if config.Enabled && config.Host != "" && config.Type != "" {
			ac.SetupClient()
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
func updateALSASettings(config client.DeviceAgentConfig) {
	var val int
	re := regexp.MustCompile(ALSAInputSourceToken)
	deviceCardMap := getDeviceToNumMappings()
	for device, card := range deviceCardMap {
		controls := getALSAControls(card)
		// For digital bridges, set all control from DeviceAgentConfig
		// For analog bridges:
		//   * if EnableUSB is false, only set the hifiberry card controls
		//   * if EnableUSB is true, set all controls
		if soundDeviceName == "dummy" || bool(config.EnableUSB) || strings.Contains(device, "hifiberry") {
			for control := range controls {
				// NOTE: When setting mute controls, use the negation (because an ALSA value of 0 means mute)
				isInputSource := re.MatchString(control)
				if strings.HasSuffix(control, "Capture Volume") {
					setALSAControl(card, control, volumeString(config.CaptureVolume, config.CaptureMute))
				} else if strings.HasSuffix(control, "Capture Switch") {
					val = boolToInt(!config.CaptureMute)
					setALSAControl(card, control, fmt.Sprintf("%d", val))
				} else if strings.HasSuffix(control, "Playback Volume") {
					// For HiFiBerry cards, always enable this "Analogue Playback Volume" option
					if strings.Contains(device, "hifiberry") && control == "Analogue Playback Volume" {
						setALSAControl(card, control, "100%")
					} else if isInputSource {
						setALSAControl(card, control, volumeString(config.MonitorVolume, config.MonitorMute))
					} else {
						setALSAControl(card, control, volumeString(config.PlaybackVolume, config.PlaybackMute))
					}
				} else if strings.HasSuffix(control, "Playback Switch") {
					if isInputSource {
						val = boolToInt(!config.MonitorMute)
						setALSAControl(card, control, fmt.Sprintf("%d", val))
					} else {
						val = boolToInt(!config.PlaybackMute)
						setALSAControl(card, control, fmt.Sprintf("%d", val))
					}
				}
			}
		}
	}
}

// setALSAControl sets the value of an ALSA control
func setALSAControl(card int, control, value string) {
	cmd := exec.Command("/usr/bin/amixer", "-c", fmt.Sprintf("%d", card), "cset", fmt.Sprintf("name='%s'", control), "--", value)
	_, err := cmd.Output()
	if err != nil {
		log.Error(err, "Unable to set ALSA control", "card", card, "control", control)
	}
	log.Info("Updated ALSA control", "card", card, "control", control, "value", value)
}

// getALSAControls returns a map of available capture/playback volume controls for a specific card
func getALSAControls(card int) map[string]bool {
	out, err := exec.Command("/usr/bin/amixer", "-c", fmt.Sprintf("%d", card), "controls").Output()
	if err != nil {
		log.Error(err, "Unable to get ALSA controls", "card", card)
		return nil
	}
	return parseALSAControls(string(out))
}

// parseALSAControls parses all relevant volume controls of an ALSA card from `amixer controls`
func parseALSAControls(output string) map[string]bool {
	controls := map[string]bool{}
	lines := strings.Split(output, "\n")
	r := regexp.MustCompile(`numid=(\d+),iface=(\w+),name='(.*(Playback Volume|Playback Switch|Capture Volume|Capture Switch))'`)
	for _, line := range lines {
		matches := r.FindAllStringSubmatch(line, -1)
		if len(matches) == 1 {
			controls[matches[0][3]] = true
		}
	}
	return controls
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
