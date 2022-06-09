package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
	"github.com/jacktrip/jacktrip-agent/pkg/common"
)

// ZitaMode is used to determine the direction the zita service
type ZitaMode string

const (
	// ZitaCapture is the zita-a2j service mode
	ZitaCapture ZitaMode = "a2j"
	// ZitaPlayback is the zita-j2a service mode
	ZitaPlayback ZitaMode = "j2a"
	// PathToAlsaState is the location of the ALSA state file for a particular device
	PathToAlsaState = "/tmp/default/asound-%s.state"
	// PathToZitaConfig is a systemd conf file path for zita
	PathToZitaConfig = "/tmp/default/zita-%s-conf"
	// ZitaConfigTemplate is a set of parameters for zita systemd
	ZitaConfigTemplate = "ZITA_OPTS=-d hw:%s -c %d -p %d -r %d -j %s\n"
	// ZitaServiceNameTemplate uses a wildcard systemd conf file
	ZitaServiceNameTemplate = "zita-%s@%s.service"
	// DetectDevicesInterval is the time to sleep between detecting new devices, in seconds
	DetectDevicesInterval = time.Second
)

// DeviceMixingManager keeps track of ephemeral states for Zita and Jack ports
type DeviceMixingManager struct {
	CurrentCaptureDevices  map[string]bool
	CurrentPlaybackDevices map[string]bool
	DeviceCardMapping      map[string]int
	DeviceStream0Mapping   map[string][]string
	mutex                  sync.Mutex
}

// Run a continuous loop performing device synchronization
func (dmm *DeviceMixingManager) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-time.After(DetectDevicesInterval):
			dmm.SynchronizeConnections(currentDeviceConfig)
		case <-ctx.Done():
			dmm.Reset()
			log.Info("Stopping device mixer")
			return
		}
	}
}

// Reset initializes DeviceMixingManager's state
func (dmm *DeviceMixingManager) Reset() {
	dmm.mutex.Lock()
	defer dmm.mutex.Unlock()

	if len(dmm.CurrentCaptureDevices) > 0 {
		for device := range dmm.CurrentCaptureDevices {
			// Stop zita service
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, ZitaCapture, device)
			killService(serviceName)
			// Remove zita config
			connectionName := fmt.Sprintf("%s-%s", ZitaCapture, device)
			os.Remove(fmt.Sprintf(PathToZitaConfig, connectionName))
			// Restore and cleanup ALSA state
			restoreAlsaState(device)
			os.Remove(fmt.Sprintf(PathToAlsaState, device))
		}
		dmm.CurrentCaptureDevices = map[string]bool{}
	}

	if len(dmm.CurrentPlaybackDevices) > 0 {
		for device := range dmm.CurrentPlaybackDevices {
			// Stop zita service
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, ZitaPlayback, device)
			killService(serviceName)
			// Remove zita config
			connectionName := fmt.Sprintf("%s-%s", ZitaPlayback, device)
			os.Remove(fmt.Sprintf(PathToZitaConfig, connectionName))
			// Restore and cleanup ALSA state
			restoreAlsaState(device)
			os.Remove(fmt.Sprintf(PathToAlsaState, device))
		}
		dmm.CurrentPlaybackDevices = map[string]bool{}
	}

	// reinitialize device lists
	if len(dmm.DeviceStream0Mapping) > 0 {
		dmm.DeviceStream0Mapping = map[string][]string{}
	}
	if len(dmm.DeviceCardMapping) > 0 {
		dmm.DeviceCardMapping = map[string]int{}
	}
}

// SynchronizeConnections synchronizes all Zita <-> Jack port connections
func (dmm *DeviceMixingManager) SynchronizeConnections(config client.DeviceAgentConfig) {
	// Reset should be called under the following conditions:
	// - multi-USB mode is disabled and the detected soundcard is not dummy (indicative of analog bridge)
	// - or device is not connected to server
	if (!config.EnableUSB && soundDeviceName != "dummy") || !config.Enabled || config.Host == "" {
		dmm.Reset()
		return
	}

	// Wait for autoconnector to be available before proceeding
	if ac == nil || ac.JackClient == nil {
		return
	}
	dmm.mutex.Lock()
	defer dmm.mutex.Unlock()

	// 1. Reset all devices-to-card information
	dmm.DeviceCardMapping = getDeviceToNumMappings()
	dmm.DeviceStream0Mapping = map[string][]string{}

	// 2. Fetch all active capture devices and get diff between active and current
	activeCaptureDevices := getCaptureDeviceNames()
	newCaptureDevices := findNewDevices(dmm.CurrentCaptureDevices, activeCaptureDevices)

	// 3. Remove stale capture devices
	removeInactiveDevices(dmm.CurrentCaptureDevices, activeCaptureDevices, ZitaCapture)

	// 4. Synchronize new capture devices
	dmm.addActiveDevices(config, newCaptureDevices, ZitaCapture)

	// 5. Fetch all active playback devices and get diff between active and current
	activePlaybackDevices := getPlaybackDeviceNames()
	newPlaybackDevices := findNewDevices(dmm.CurrentPlaybackDevices, activePlaybackDevices)

	// 6. Remove stale playback devices
	removeInactiveDevices(dmm.CurrentPlaybackDevices, activePlaybackDevices, ZitaPlayback)

	// 7. Synchronize new playback devices
	dmm.addActiveDevices(config, newPlaybackDevices, ZitaPlayback)

	// 8. Update ALSA settings
	if len(newCaptureDevices) > 0 || len(newPlaybackDevices) > 0 {
		updateALSASettings(config)
	}
}

func (dmm *DeviceMixingManager) connectZita(mode ZitaMode, device string, config client.DeviceAgentConfig) error {
	// check if the device has support for the server sampleRate
	stream0, ok := dmm.DeviceStream0Mapping[device]
	if !ok {
		log.Info("Stream0 info does not exist", "device", device)
		return nil
	}

	sampleRateToChannels := getSampleRateToChannelMap(stream0, mode)
	targetSampleRate, channelCount := findBestSampleRateAndChannel(sampleRateToChannels, config.SampleRate)
	if channelCount == -1 {
		log.Info(fmt.Sprintf("Channel count was not found for %s. Connection cannot not be established.", device))
		return nil
	}

	// write a systemd config file for Zita Bridge parameters
	if err := writeZitaConfig(channelCount, config.Period, targetSampleRate, mode, device); err != nil {
		log.Error(err, err.Error())
		return err
	}

	/*
		Start Zita Bridge service as a systemd service
		Note:
		- Zita connection commands are long running commands that don't exit even if the device connection exits.
		- Therefore, systemd service around Zita should be stopped explicitly
	*/
	serviceName := fmt.Sprintf(ZitaServiceNameTemplate, mode, device)
	if err := StartZitaService(serviceName); err != nil {
		log.Error(err, err.Error())
		return err
	}

	return nil
}

// addInactiveDevice starts Zita processes for each new, active device detected
func (dmm *DeviceMixingManager) addActiveDevices(config client.DeviceAgentConfig, newDevices []string, mode ZitaMode) {
	currentDevices := dmm.CurrentPlaybackDevices
	if mode == ZitaCapture {
		currentDevices = dmm.CurrentCaptureDevices
	}
	for _, device := range newDevices {
		// read card num; if card num doesn't exist, don't connect
		cardNum, ok := dmm.DeviceCardMapping[device]
		if !ok {
			continue
		}

		// if device stream0 doesn't exist, read card stream0
		_, ok = dmm.DeviceStream0Mapping[device]
		if !ok {
			dmm.DeviceStream0Mapping[device] = readCardStream0(cardNum)
		}

		// write the current state of the device to a file
		storeAlsaState(device)

		// establish zita <-> JACK connections
		if err := dmm.connectZita(mode, device, config); err == nil {
			currentDevices[device] = true
		}
	}
}

// findNewDevices returns a list of new devices that are not in the current list
func findNewDevices(foundDevices, activeDevices map[string]bool) []string {
	var newDevices []string
	for device := range activeDevices {
		if _, ok := foundDevices[device]; !ok {
			newDevices = append(newDevices, device)
		}
	}
	return newDevices
}

// removeInactiveDevices removes devices that are no longer active
func removeInactiveDevices(foundDevices, activeDevices map[string]bool, mode ZitaMode) {
	for device := range foundDevices {
		if _, ok := activeDevices[device]; !ok {
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, mode, device)
			StopZitaService(serviceName)
			delete(foundDevices, device)
		}
	}
}

func writeZitaConfig(numChannel int, period int, rate int, mode ZitaMode, device string) error {
	// format a path with a device and mode specific name
	connectionName := fmt.Sprintf("%s-%s", mode, device)
	path := fmt.Sprintf(PathToZitaConfig, connectionName)

	// format a config template
	zitaConfig := fmt.Sprintf(ZitaConfigTemplate, device, numChannel, period, rate, connectionName)
	return writeConfig(path, zitaConfig)
}

func writeConfig(path string, content string) error {
	if err := ioutil.WriteFile(path, []byte(content), 0644); err != nil {
		log.Error(err, "Error while writing config")
		return err
	}
	return nil
}

func storeAlsaState(device string) error {
	stateFile := fmt.Sprintf(PathToAlsaState, device)
	if _, err := os.Stat(stateFile); errors.Is(err, os.ErrNotExist) {
		_, err := exec.Command("alsactl", "store", "--file", stateFile, device).Output()
		if err != nil {
			log.Error(err, "Unable to store device state")
			return err
		}
	}
	return nil
}

func restoreAlsaState(device string) error {
	stateFile := fmt.Sprintf(PathToAlsaState, device)
	if _, err := os.Stat(stateFile); err == nil {
		_, err := exec.Command("alsactl", "restore", "--file", stateFile, device).Output()
		if err != nil {
			log.Error(err, "Unable to restore device state")
			return err
		}
	}
	return nil
}

func getCaptureDeviceNames() map[string]bool {
	out, err := exec.Command("arecord", "-l").Output()
	if err != nil {
		log.Error(err, "Unable to retrieve capture device names")
		return nil
	}
	return extractNames(string(out))
}

func getPlaybackDeviceNames() map[string]bool {
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		log.Error(err, "Unable to retrieve playback device names")
		return nil
	}
	return extractNames(string(out))
}

func getDeviceToNumMappings() map[string]int {
	out, err := exec.Command("cat", "/proc/asound/cards").Output()
	if err != nil {
		log.Error(err, "Unable to retrieve playback device names")
		return nil
	}
	return extractCardNum(string(out))
}

func readCardStream0(cardNum int) []string {
	out, err := exec.Command("cat", fmt.Sprintf("/proc/asound/card%d/stream0", cardNum)).Output()
	if err != nil {
		log.Error(err, fmt.Sprintf("Unable to retrieve card information for card %d", cardNum))
		return nil
	}
	return strings.Split(string(out), "\n")
}

// findBestSampleRateAndChannel returns the best sample rate & channel count based on a desired target
func findBestSampleRateAndChannel(rateToChannelsMap map[int]int, desiredSampleRate int) (int, int) {
	if len(rateToChannelsMap) == 0 {
		return 0, -1
	}
	if bestChannel, ok := rateToChannelsMap[desiredSampleRate]; ok {
		return desiredSampleRate, bestChannel
	}
	// If the desired sample rate is not available, fallback to either 48k or 44.1k
	if bestChannel, ok := rateToChannelsMap[48000]; ok {
		return 48000, bestChannel
	}
	if bestChannel, ok := rateToChannelsMap[44100]; ok {
		return 44100, bestChannel
	}
	return 0, -1
}

// getSampleRateToChannelMap returns a map of sample-rates-to-channel-counts for an ALSA card from `/proc/asound/card%d/stream0`
func getSampleRateToChannelMap(sentences []string, mode ZitaMode) map[int]int {
	output := map[int]int{}
	// When scanning through the output, we begin parsing each line beginning at `startSequence` and ending at `stopSequence`
	startSequence := "Playback:"
	stopSequence := "Capture:"
	if mode == ZitaCapture {
		startSequence = "Capture:"
		stopSequence = "Playback:"
	}
	for i, sentence := range sentences {
		// when we find the `startSequence` section, scan each subsequent line for any meaningful bits of data
		if strings.Contains(sentence, startSequence) {
			for j := i + 1; j < len(sentences); j++ {
				currSentence := sentences[j]
				// break when we see the `startSequence` because that indicates the end of the section
				if strings.Contains(currSentence, stopSequence) {
					return output
				}
				// if we found our target sampleRate, look for the number of channels
				if strings.Contains(currSentence, "Rates:") {
					sampleRates := parseSampleRates(currSentence)
					// parse the interface's channels
					r := regexp.MustCompile(`Channels: (\d)`)
					for ii := j - 1; ii >= common.Max(0, j-5); ii-- {
						currSentence := sentences[ii]
						subMatch := r.FindStringSubmatch(currSentence)
						if len(subMatch) > 1 {
							n, err := strconv.Atoi(subMatch[1])
							if err != nil {
								continue
							}
							for _, rate := range sampleRates {
								output[rate] = n
							}
						}
					}
				}

			}
		}
	}
	return output
}

func extractNames(target string) map[string]bool {
	names := map[string]bool{}
	sentences := strings.Split(target, "\n")
	r := regexp.MustCompile(`^card \d+: (\w+) \[`)
	for _, sentence := range sentences {
		subMatch := r.FindStringSubmatch(sentence)
		if len(subMatch) > 1 && subMatch[1] != "sndrpihifiberry" { // exclude hifiberry since we won't use it
			names[subMatch[1]] = true
		}
	}
	return names
}

func extractCardNum(target string) map[string]int {
	nameToNum := map[string]int{}
	sentences := strings.Split(target, "\n")
	r := regexp.MustCompile(`^ (\d+) \[(\w+)\s*\]`)
	for _, sentence := range sentences {
		result := r.FindAllStringSubmatch(sentence, -1)
		if len(result) == 1 {
			num, err := strconv.Atoi(result[0][1])
			if err == nil {
				nameToNum[result[0][2]] = num
			}
		}
	}
	return nameToNum
}

// parseSampleRates parses the sample rate(s) line of an ALSA card from `/proc/asound/card%d/stream0`
func parseSampleRates(line string) []int {
	sampleRates := []int{}
	r := regexp.MustCompile(`Rates: (\d+)(?:,\s(\d+))?`)
	rates := r.FindStringSubmatch(line)
	if len(rates) <= 1 {
		return sampleRates
	}
	for _, rate := range rates[1:] {
		currSampleRate, err := strconv.Atoi(rate)
		if err != nil {
			continue
		}
		sampleRates = append(sampleRates, currSampleRate)
	}
	return sampleRates
}
