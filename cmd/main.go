// Copyright 2020 20hz, LLC
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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/go-logr/zapr"
	goping "github.com/go-ping/ping"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// LocalHTTPServer is the <server>:port the HTTP server listens on
	LocalHTTPServer = ":80"

	// AgentPingURL is the URL used to POST agent pings
	AgentPingURL = "/agents/ping"

	// AgentPingInterval is number of seconds in between agent ping requests
	AgentPingInterval = 5

	// MaxClientsPerProcessor is the number of JackTrip clients to support per logical processor
	MaxClientsPerProcessor = 6 // add 1 (20%) for Jamulus bridge, etc

	// JackServiceName is the name of the systemd service for Jack
	JackServiceName = "jack.service"

	// SuperColliderServiceName is the name of the systemd service for the SuperCollider server
	SuperColliderServiceName = "supernova.service"

	// SCLangServiceName is the name of the systemd service for the SuperCollider language runtime
	SCLangServiceName = "sclang.service"

	// JackAutoconnectServiceName is the name of the systemd service for connecting jack clients
	JackAutoconnectServiceName = "jack-autoconnect.service"

	// JackPlumbingServiceName is the name of the systemd service for connecting jack clients
	JackPlumbingServiceName = "jack-plumbing.service"

	// JackTripReceiveServiceName is the name of the systemd service for connecting jack client inputs
	JackTripReceiveServiceName = "jacktrip-receive.service"

	// JackTripSendServiceName is the name of the systemd service for connecting jack client outputs
	JackTripSendServiceName = "jacktrip-send.service"

	// JackTripServiceName is the name of the systemd service for JackTrip
	JackTripServiceName = "jacktrip.service"

	// JamulusServiceName is the name of the systemd service for Jamulus client on RPI devices
	JamulusServiceName = "jamulus.service"

	// JamulusServerServiceName is the name of the systemd service for the Jamulus server
	JamulusServerServiceName = "jamulus-server.service"

	// JamulusBridgeServiceName is the name of the systemd service for the Jamulus -> JackTrip  bridge
	JamulusBridgeServiceName = "jamulus-bridge.service"

	// PathToJackConfig is the path to Jack service config file
	PathToJackConfig = "/tmp/default/jack"

	// PathToJackTripConfig is the path to JackTrip service config file
	PathToJackTripConfig = "/tmp/default/jacktrip"

	// PathToJamulusConfig is the path to Jamulus service config file
	PathToJamulusConfig = "/tmp/default/jamulus"

	// PathToSCLangConfig is the path to SuperCollider sclang service config file
	PathToSCLangConfig = "/tmp/default/sclang"

	// PathToSuperColliderConfig is the path to SuperCollider service config file
	PathToSuperColliderConfig = "/tmp/default/supercollider"

	// PathToSuperColliderStartupFile is the path to SuperCollider startup file
	PathToSuperColliderStartupFile = "/tmp/jacktrip.scd"

	// PathToAvahiServiceFile is the path to the avahi service file for jacktrip-agent
	PathToAvahiServiceFile = "/tmp/avahi/services/jacktrip-agent.service"

	// JackDeviceConfigTemplate is the template used to generate /tmp/default/jack file on raspberry pi devices
	JackDeviceConfigTemplate = "JACK_OPTS=-dalsa -dhw:%s --rate %d --period %d\n"

	// JackServerConfigTemplate is the template used to generate /tmp/default/jack file on audio servers
	JackServerConfigTemplate = "JACK_OPTS=-d dummy --rate %d --period %d\n"

	// JackTripDeviceConfigTemplate is the template used to generate /tmp/default/jacktrip file  on raspberry pi devices
	JackTripDeviceConfigTemplate = "JACKTRIP_OPTS=-t -z -n %d -C %s --peerport %d --bindport %d --clientname hubserver --remotename %s %s\n"

	// JackTripServerConfigTemplate is the template used to generate /tmp/default/jacktrip file on audio servers
	JackTripServerConfigTemplate = "JACKTRIP_OPTS=-S -t -z --bindport %d --nojackportsconnect %s\n"

	// JamulusDeviceConfigTemplate is the template used to generate /tmp/default/jamulus file on raspberry pi devices
	JamulusDeviceConfigTemplate = "JAMULUS_OPTS=-n -i /tmp/jamulus.ini -c %s:%d\n"

	// SCLangConfigTemplate is the template used to generate /tmp/default/sclang file on audio servers
	SCLangConfigTemplate = "SCLANG_OPTS=%s %s\n"

	// SuperColliderConfigTemplate is the template used to generate /tmp/default/supercollider file on audio servers
	SuperColliderConfigTemplate = "SC_OPTS=-i %d -o %d -m %d -z %d -a 2570\n"

	// DevicesRedirectURL is a template used to construct UI redirect URL for this device
	DevicesRedirectURL = "https://app.jacktrip.org/devices/%s?apiPrefix=%s&apiHash=%s"

	// EC2InstanceIDURL is url using EC2 metadata service that returns the instance-id
	// See https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html
	EC2InstanceIDURL = "http://169.254.169.254/latest/meta-data/instance-id"

	// PathToMACAddress is the path to ethernet device MAC address, via Linux kernel
	PathToMACAddress = "/sys/class/net/eth0/address"

	// PathToAsoundCards is the path to the ALSA card list
	PathToAsoundCards = "/proc/asound/cards"

	// AgentConfigDir is the directory containing agent config files
	AgentConfigDir = "/etc/jacktrip"

	// AgentLibDir is the directory containing additional files used by the agent
	AgentLibDir = "/var/lib/jacktrip"

	// SecretBytes are used to generate random secret strings
	SecretBytes = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

//var log = stdr.New(stdlog.New(os.Stderr, "", stdlog.LstdFlags|stdlog.Lshortfile)).WithName("jacktrip.agent")
var zLogger, _ = zap.NewProduction()
var log = zapr.NewLogger(zLogger).WithName("jacktrip.agent")
var soundDeviceName = ""
var soundDeviceType = ""

func init() {
	// seed random number generator for secret generation
	rand.Seed(time.Now().UnixNano())
}

// main wires everything together and starts up the Agent server
func main() {

	// require this be run as root
	if os.Geteuid() != 0 {
		log.Info("jacktrip-agent must be run as root")
		os.Exit(1)
	}

	apiOrigin := flag.String("o", "https://app.jacktrip.org/api", "origin to use when constructing API endpoints")
	serverMode := flag.Bool("s", false, "true if running on audio server; false if on device")
	flag.Parse()

	if *serverMode {
		runOnServer(*apiOrigin)
	} else {
		runOnDevice(*apiOrigin)
	}

	log.Info("Exiting")
}

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
	go runHTTPServer(&wg, router)

	// update avahi service config and restart daemon
	ping := client.AgentPing{
		AgentCredentials: credentials,
		MAC:              mac,
		Version:          getPatchVersion(),
		Type:             soundDeviceType,
	}
	updateAvahiServiceConfig(ping, "starting")

	// start ping server to send pings and update agent config
	wg.Add(1)
	go runPingServer(&wg, ping, apiOrigin)

	// wait for everything to complete
	wg.Wait()
}

// runOnServer is used to run jacktrip-agent on an audio cloud server
func runOnServer(apiOrigin string) {
	// setup wait group for multiple routines
	var wg sync.WaitGroup

	log.Info("Running jacktrip-agent in server mode")

	// start HTTP server to respond to pings
	router := mux.NewRouter()
	router.HandleFunc("/ping", handlePingRequest).Methods("GET")
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		OptionsGetOnly(w, r)
	})).Methods("OPTIONS")
	wg.Add(1)
	go runHTTPServer(&wg, router)

	// get cloud id
	cloudID := getCloudID()

	// TODO: get credentials
	credentials := client.AgentCredentials{}

	// start ping server to send pings and update agent config
	ping := client.AgentPing{
		AgentCredentials: credentials,
		CloudID:          cloudID,
		Version:          getPatchVersion(),
	}
	wg.Add(1)
	go runPingServer(&wg, ping, apiOrigin)

	// wait for everything to complete
	wg.Wait()
}

// getCloudID retrieves cloud instance id, via metadata service
func getCloudID() string {
	// send request to get instance-id from EC2 metadata
	r, err := http.Get(EC2InstanceIDURL)
	if err != nil {
		log.Error(err, "Failed to retrieve instance-id from metadata service")
		panic(err)
	}
	defer r.Body.Close()

	// check response status
	if r.StatusCode != http.StatusOK {
		err = errors.New("Failed to retrieve instance-id from metadata service")
		log.Error(err, "Bad status code", "status", r.StatusCode)
		panic(err)
	}

	// decode config from response
	bodyBytes, err := ioutil.ReadAll(r.Body)
	cloudID := string(bodyBytes)
	if err != nil || cloudID == "" {
		err = errors.New("Failed to retrieve instance-id from metadata service")
		log.Error(err, "Empty response")
		panic(err)
	}

	log.Info("Retrieved instance-id", "id", cloudID)

	return cloudID
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

// getCredentials retrieves jacktrip agent credentials from system config file.
// If config does not exist, it will generate and save new credentials to config file.
func getCredentials() client.AgentCredentials {

	rawBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/credentials", AgentConfigDir))
	if err != nil {
		log.Error(err, "Failed to read credentials")
		panic(err)
	}

	splits := bytes.Split(bytes.TrimSpace(rawBytes), []byte("."))
	if len(splits) != 2 || len(splits[0]) < 1 || len(splits[1]) < 1 {
		log.Error(err, "Failed to parse credentials")
		panic(err)
	}

	return client.AgentCredentials{
		APIPrefix: string(splits[0]),
		APISecret: string(splits[1]),
	}
}

// runPingServer sends pings to service and manages config updates
func runPingServer(wg *sync.WaitGroup, ping client.AgentPing, apiOrigin string) {
	defer wg.Done()

	log.Info("Starting agent ping server")

	lastStatus := "starting"
	config := client.AgentConfig{}
	lastConfig := config
	getPingStats(&ping, nil)

	for {
		// update and encode ping content
		pingBytes, err := json.Marshal(ping)
		if err != nil {
			log.Error(err, "Failed to marshal agent ping request")
			if ping.CloudID == "" && lastStatus != "error" {
				updateAvahiServiceConfig(ping, "error")
				lastStatus = "error"
			}
			panic(err)
		}

		// send ping request
		r, err := http.Post(fmt.Sprintf("%s%s", apiOrigin, AgentPingURL), "application/json", bytes.NewReader(pingBytes))
		if err != nil {
			log.Error(err, "Failed to send agent ping request")
			if ping.CloudID == "" && lastStatus != "error" {
				updateAvahiServiceConfig(ping, "error")
				lastStatus = "error"
			}
			time.Sleep(time.Second * AgentPingInterval)
			continue
		}
		defer r.Body.Close()

		// check response status
		if r.StatusCode != http.StatusOK {
			log.Info("Bad response from agent ping", "status", r.StatusCode)
			if ping.CloudID == "" && lastStatus != "error" {
				updateAvahiServiceConfig(ping, "error")
				lastStatus = "error"
			}
			time.Sleep(time.Second * AgentPingInterval)
			continue
		}

		// decode config from response
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&config); err != nil {
			log.Error(err, "Failed to unmarshal agent ping response")
			if ping.CloudID == "" && lastStatus != "error" {
				updateAvahiServiceConfig(ping, "error")
				lastStatus = "error"
			}
			time.Sleep(time.Second * AgentPingInterval)
			continue
		}

		// check if config has changed
		if config != lastConfig {
			log.Info("Config updated", "value", config)

			// update ALSA card settings
			if ping.CloudID == "" && config.ALSAConfig != lastConfig.ALSAConfig {
				updateALSASettings(config)
			}

			// check if ALSA card settings was the only change
			lastConfig.ALSAConfig = config.ALSAConfig
			if config != lastConfig {

				// check if supercollider code was the only change
				lastConfig.MixBranch = config.MixBranch
				lastConfig.MixCode = config.MixCode
				if config == lastConfig {

					// only the supercollider code needs updated
					// update configs and restart sclang service to minimize disruption
					updateSuperColliderConfigs(config)
					err = restartService(SCLangServiceName)
					if err != nil {
						log.Error(err, "Unable to restart service", "name", SCLangServiceName)
					}

				} else {
					// more changes required -> reset everything

					// update managed config files
					updateServiceConfigs(config, strings.Replace(ping.MAC, ":", "", -1), ping.CloudID != "")

					// shutdown or restart managed services
					restartAllServices(config, ping.CloudID != "")
				}
			}

			lastConfig = config
		}

		// update device status in avahi service config, if necessary
		if ping.CloudID == "" {
			status := "not connected"
			if config.Enabled {
				status = "connected"
			}
			if lastStatus != status {
				updateAvahiServiceConfig(ping, status)
				lastStatus = status
			}
		}

		if ping.CloudID == "" && config.Enabled && config.Host != "" {
			// ping server instead of sleeping
			pinger, err := goping.NewPinger(config.Host)
			if err == nil {
				log.V(2).Info("Pinging server", "host", config.Host)
				pinger.Count = AgentPingInterval * 10
				pinger.Interval = time.Millisecond * 100
				pinger.Timeout = time.Second * AgentPingInterval
				pinger.Run()
				log.V(1).Info("Done pinging server", "host", config.Host, "stats", *pinger.Statistics())
				getPingStats(&ping, pinger.Statistics())
			} else {
				log.Error(err, "Failed to create pinger")
				// sleep in between pings
				time.Sleep(time.Second * AgentPingInterval)
				getPingStats(&ping, nil)
			}
		} else {
			// sleep in between pings
			time.Sleep(time.Second * AgentPingInterval)
			getPingStats(&ping, nil)
		}
	}
}

// getPingStats updates and AgentPing message with go-ping Pinger stats
func getPingStats(ping *client.AgentPing, stats *goping.Statistics) {
	ping.StatsUpdatedAt = time.Now()

	if stats == nil {
		ping.PacketsRecv = 0
		ping.PacketsSent = 0
		ping.MinRtt = 0
		ping.MaxRtt = 0
		ping.AvgRtt = 0
		ping.StdDevRtt = 0
		return
	}

	ping.PacketsRecv = stats.PacketsRecv
	ping.PacketsSent = stats.PacketsSent
	ping.MinRtt = stats.MinRtt
	ping.MaxRtt = stats.MaxRtt
	ping.AvgRtt = stats.AvgRtt
	ping.StdDevRtt = stats.StdDevRtt
}

// updateALSASettings is used to update the settings for an ALSA sound card
func updateALSASettings(config client.AgentConfig) {
	switch soundDeviceType {
	case "snd_rpi_hifiberry_dacplusadc":
		fallthrough
	case "snd_rpi_hifiberry_dacplusadcpro":
		updateALSASettingsHiFiBerry(config)
		break
	case "audioinjector-pi-soundcard":
		updateALSASettingsAudioInjector(config)
		break
	case "USB Audio Device":
		updateALSASettingsUSBAudioDevice(config)
		break
	case "USB PnP Sound Device":
		updateALSASettingsUSBPnPSoundDevice(config)
		break
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

// updateSuperColliderConfigs is used to update SuperCollider config files on managed audio servers
func updateSuperColliderConfigs(config client.AgentConfig) {
	// write SuperCollider (server) config file
	maxClients := runtime.NumCPU() * MaxClientsPerProcessor
	numInputChannels := maxClients * 2
	numOutputChannels := maxClients * 2
	scMemorySize := 8192
	if maxClients > 50 {
		scMemorySize = 16384
	}
	scBufSize := 64
	if config.Period < 64 {
		scBufSize = config.Period
	}
	scConfig := fmt.Sprintf(SuperColliderConfigTemplate, numInputChannels, numOutputChannels, scMemorySize, scBufSize)
	err := ioutil.WriteFile(PathToSuperColliderConfig, []byte(scConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save SuperCollider config", "path", PathToSuperColliderConfig)
	}

	// write SuperCollider (sclang) config file
	sclangConfig := fmt.Sprintf(SCLangConfigTemplate, config.MixBranch, PathToSuperColliderStartupFile)
	err = ioutil.WriteFile(PathToSCLangConfig, []byte(sclangConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save sclang config", "path", PathToSCLangConfig)
	}

	// write SuperCollider startup file
	scStartup := fmt.Sprintf(`~maxClients = %d;
~inputChannelsPerClient = 2;
~outputChannelsPerClient = 2;
%s`, maxClients, config.MixCode)
	err = ioutil.WriteFile(PathToSuperColliderStartupFile, []byte(scStartup), 0644)
	if err != nil {
		log.Error(err, "Failed to save SuperCollider startup file", "path", PathToSuperColliderStartupFile)
	}
}

// updateServiceConfigs is used to update config for managed systemd services
func updateServiceConfigs(config client.AgentConfig, remoteName string, isServer bool) {

	// assume auto queue unless > 0
	jackTripExtraOpts := "-q auto"
	if config.QueueBuffer > 0 {
		jackTripExtraOpts = fmt.Sprintf("-q %d", config.QueueBuffer)
	}

	// create config opts from templates
	var jackConfig, jackTripConfig string
	if isServer {
		jackConfig = fmt.Sprintf(JackServerConfigTemplate, config.SampleRate, config.Period)
		jackTripConfig = fmt.Sprintf(JackTripServerConfigTemplate, config.Port, jackTripExtraOpts)
	} else {
		updateJamulusIni(config)

		jackConfig = fmt.Sprintf(JackDeviceConfigTemplate, soundDeviceName, config.SampleRate, config.Period)

		// configure limiter
		if config.Limiter {
			jackTripExtraOpts = fmt.Sprintf("%s -Oio", jackTripExtraOpts)
		}

		// configure effects
		jackTripEffects := ""
		if config.Compressor {
			jackTripEffects = "o:c"
		}
		if config.Reverb > 0 {
			reverbFloat := float32(config.Reverb) / 100
			jackTripEffects = fmt.Sprintf("%s i:f(%f)", jackTripEffects, reverbFloat)
		}
		if jackTripEffects != "" {
			jackTripExtraOpts = fmt.Sprintf("%s -f \"%s\"", jackTripExtraOpts, strings.TrimSpace(jackTripEffects))
		}

		// check if loopback
		//hubpatch := 2
		//if config.LoopBack {
		//	hubpatch = 4
		//}

		// check if stereo
		channels := 1
		if config.Stereo {
			channels = 2
		}

		jackTripConfig = fmt.Sprintf(JackTripDeviceConfigTemplate, channels, config.Host, config.Port, config.DevicePort, remoteName, strings.TrimSpace(jackTripExtraOpts))
	}

	// ensure config directory exists
	err := os.MkdirAll("/tmp/default", 0755)
	if err != nil {
		log.Error(err, "Failed to create directory", "path", "/tmp/default")
		panic(err)
	}

	// write jack config file
	err = ioutil.WriteFile(PathToJackConfig, []byte(jackConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save Jack config", "path", PathToJackConfig)
		panic(err)
	}

	// write JackTrip config file
	err = ioutil.WriteFile(PathToJackTripConfig, []byte(jackTripConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save JackTrip config", "path", PathToJackTripConfig)
	}

	// write Jamulus config file
	jamulusConfig := fmt.Sprintf(JamulusDeviceConfigTemplate, config.Host, config.Port)
	err = ioutil.WriteFile(PathToJamulusConfig, []byte(jamulusConfig), 0644)
	if err != nil {
		log.Error(err, "Failed to save Jamulus config", "path", PathToJamulusConfig)
	}

	if isServer {
		// update SuperCollider config files
		updateSuperColliderConfigs(config)
	}
}

// updateJamulusIni writes a new /tmp/jamulus.ini file using template at /var/lib/jacktrip/jamulus.ini
func updateJamulusIni(config client.AgentConfig) {
	srcFileName := "/var/lib/jacktrip/jamulus.ini"
	srcFile, err := os.Open(srcFileName)
	if err != nil {
		log.Error(err, "Failed to open file for reading", "path", srcFileName)
	}
	defer srcFile.Close()

	dstFileName := "/tmp/jamulus.ini"
	dstFile, err := os.OpenFile(dstFileName, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Error(err, "Failed to open file for writing", "path", dstFileName)
	}
	defer dstFile.Close()

	quality := 2
	if config.Quality == 0 {
		quality = 0
	}

	writer := bufio.NewWriter(dstFile)
	scanner := bufio.NewScanner(srcFile)
	audioQualityRx := regexp.MustCompile(`.*<audioquality>.*</audioquality>.*`)

	writeToFile := func() {
		line := scanner.Text()
		if audioQualityRx.MatchString(line) {
			line = fmt.Sprintf("<audioquality>%d</audioquality>", quality)
		}
		_, err = writer.WriteString(line + "\n")
		if err != nil {
			log.Error(err, "Error writing to file", "path", dstFileName)
		}
	}

	for scanner.Scan() {
		writeToFile()
	}

	if err := scanner.Err(); err != nil {
		log.Error(err, "Error reading file", "path", srcFileName)
	}

	writeToFile()
	writer.Flush()
}

// updateAvahiServiceConfig generates a new /etc/avahi/services/jacktrip-agent.service file
func updateAvahiServiceConfig(ping client.AgentPing, status string) {
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
}

// restartAllServices is used to restart all of the managed systemd services
func restartAllServices(config client.AgentConfig, isServer bool) {
	// create dbus connection to manage systemd units
	conn, err := dbus.New()
	if err != nil {
		log.Error(err, "Failed to connect to dbus")
		panic(err)
	}
	defer conn.Close()

	// stop any managed services that are active
	units, err := conn.ListUnitsByNames([]string{JackServiceName, SuperColliderServiceName, SCLangServiceName,
		JackAutoconnectServiceName, JackPlumbingServiceName, JackTripReceiveServiceName, JackTripSendServiceName,
		JackTripServiceName, JamulusServiceName, JamulusServerServiceName, JamulusBridgeServiceName})
	if err != nil {
		log.Error(err, "Failed to get status of managed services")
		panic(err)
	}
	for _, u := range units {
		err = stopService(conn, u)
		if err != nil {
			log.Error(err, "Unable to stop service")
			panic(err)
		}
	}

	// don't restart if server is not active
	if !config.Enabled {
		return
	}

	// determine which services to start
	var servicesToStart []string
	switch config.Type {
	case client.JackTrip:
		servicesToStart = []string{JackServiceName, JackTripServiceName}
		if isServer {
			if runtime.NumCPU() > 36 {
				servicesToStart = append(servicesToStart, JackTripReceiveServiceName, JackTripSendServiceName, JackPlumbingServiceName)
			} else {
				servicesToStart = append(servicesToStart, SuperColliderServiceName, SCLangServiceName, JackAutoconnectServiceName)
			}
		}
	case client.Jamulus:
		if isServer {
			servicesToStart = []string{JackServiceName, JamulusServerServiceName}
		} else {
			servicesToStart = []string{JackServiceName, JamulusServiceName}
		}
	case client.JackTripJamulus:
		if isServer {
			servicesToStart = []string{JackServiceName, JackTripServiceName}
			servicesToStart = append(servicesToStart, JamulusServerServiceName, JamulusBridgeServiceName)
			if runtime.NumCPU() > 36 {
				servicesToStart = append(servicesToStart, JackTripReceiveServiceName, JackTripSendServiceName, JackPlumbingServiceName)
			} else {
				servicesToStart = append(servicesToStart, SuperColliderServiceName, SCLangServiceName, JackAutoconnectServiceName)
			}
		} else {
			switch config.Quality {
			case 0:
				servicesToStart = []string{JackServiceName, JamulusServiceName}
			case 1:
				servicesToStart = []string{JackServiceName, JamulusServiceName}
			case 2:
				servicesToStart = []string{JackServiceName, JackTripServiceName}
			}
		}
	}

	// start managed services
	for _, serviceName := range servicesToStart {
		err = startService(conn, serviceName)
		if err != nil {
			log.Error(err, "Unable to start service", "name", serviceName)
			panic(err)
		}
	}
}

// stopService is used to stop a managed systemd service
func stopService(conn *dbus.Conn, u dbus.UnitStatus) error {
	if u.ActiveState == "inactive" {
		return nil
	}

	log.Info("Stopping managed service", "service", u.Name)

	reschan := make(chan string)
	_, err := conn.StopUnit(u.Name, "replace", reschan)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: job status=%s", u.Name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("Failed to stop %s: job status=%s", u.Name, jobStatus)
	}

	return nil
}

// startService is used to start a managed systemd service
func startService(conn *dbus.Conn, name string) error {
	log.Info("Starting managed service", "service", name)

	reschan := make(chan string)
	_, err := conn.StartUnit(name, "replace", reschan)
	if err != nil {
		return fmt.Errorf("Failed to start %s: job status=%s", name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("Failed to start %s: job status=%s", name, jobStatus)
	}

	return nil
}

// startService is used to restart a managed systemd service
func restartService(name string) error {
	log.Info("Restarting managed service", "service", name)

	// create dbus connection to manage systemd units
	conn, err := dbus.New()
	if err != nil {
		return errors.New("Failed to connect to dbus")
	}
	defer conn.Close()

	reschan := make(chan string)
	_, err = conn.RestartUnit(name, "replace", reschan)
	if err != nil {
		return fmt.Errorf("Failed to restart %s: job status=%s", name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("Failed to restart %s: job status=%s", name, jobStatus)
	}

	return nil
}

// runHTTPServer runs the agent's HTTP server
func runHTTPServer(wg *sync.WaitGroup, router *mux.Router) error {
	defer wg.Done()
	log.Info("Starting agent HTTP server")
	err := http.ListenAndServe(LocalHTTPServer, router)
	if err != nil {
		log.Error(err, "HTTP server error")
	}
	return err
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

// handlePingRequest upgrades ping request to a websocket responder
func handlePingRequest(w http.ResponseWriter, r *http.Request) {
	// return success if no request for websocket
	if r.Header.Get("Connection") != "Upgrade" || r.Header.Get("Upgrade") != "websocket" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
		return
	}

	// upgrade to websocket
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err, "Unable to upgrade to websocket")
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Error(err, "Unable to read websocket message")
			break
		}
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Error(err, "Unable to write websocket message")
			break
		}
	}
}

// OptionsGetOnly responds with a list of allow methods for Get only
func OptionsGetOnly(w http.ResponseWriter, r *http.Request) {
	allowMethods := "GET, OPTIONS"
	w.Header().Set("Allow", allowMethods)
	w.Header().Set("Access-Control-Allow-Methods", allowMethods)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
}

// RespondJSON makes the response with payload as json format
func RespondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	w.Write([]byte(response))
}
