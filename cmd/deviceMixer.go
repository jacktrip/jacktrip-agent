package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// PathToZitaConfig is a systemd conf file path for zita
	PathToZitaConfig = "/tmp/default/zita-%s-conf"
	// PathToJackConnectConfig is a systemd conf file path for jack-connect
	PathToJackConnectConfig = "/tmp/default/jack-connect-%s-conf"
	// ZitaConfigTemplate is a set of parameters for zita systemd
	ZitaConfigTemplate = "ZITA_OPTS=-d hw:%s -c %d -r %d -j %s\n"
	// ZitaA2JServiceNameTemplate uses a wildcard systemd conf file
	ZitaA2JServiceNameTemplate = "zita-a2j-@%s.service"
	// ZitaJ2AServiceNameTemplate uses a wildcard systemd conf file
	ZitaJ2AServiceNameTemplate = "zita-j2a-@%s.service"
	// ZitaServiceNameTemplate uses a wildcard systemd conf file
	ZitaServiceNameTemplate = "zita-%s-@%s.service"
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
	shutdown               chan struct{}
}

// NewReaper starts and returns a new instance of a cloud instance Reaper
func NewDeviceMixingManager() *DeviceMixingManager {
	// reinitialize device lists
	dmm := &DeviceMixingManager{
		CurrentCaptureDevices:  map[string]bool{},
		CurrentPlaybackDevices: map[string]bool{},
		DeviceStream0Mapping:   map[string][]string{},
		DeviceCardMapping:      map[string]int{},
		shutdown:               make(chan struct{}),
	}
	return dmm
}

// Stop is used to halt the mixer manager
func (dmm *DeviceMixingManager) Stop() {
	close(dmm.shutdown)
}

// Run a continuous loop performing device synchronization
func (dmm *DeviceMixingManager) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	// ticker used to process retries
	//t := time.NewTicker(time.Second)
	//defer t.Stop()

	// loop until signal or channel is closed
	for {
		select {
		case <-time.After(DetectDevicesInterval):
			dmm.SynchronizeConnections(currentDeviceConfig)
		case <-dmm.shutdown:
			// exit if channel was closed
			log.Info("Exiting due to shutdown request")
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
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "a2j", device)
			StopZitaService(serviceName)
		}
		dmm.CurrentCaptureDevices = map[string]bool{}
	}

	if len(dmm.CurrentPlaybackDevices) > 0 {
		for device := range dmm.CurrentPlaybackDevices {
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "j2a", device)
			StopZitaService(serviceName)
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

// TODO: We should probably have error handling here
// SynchronizeConnections synchronizes all Zita <-> Jack port connections
func (dmm *DeviceMixingManager) SynchronizeConnections(config client.AgentConfig) {
	// do nothing when the device is not connect to server
	if !config.Enabled || config.Host == "" {
		dmm.Reset()
		return
	}

	// Wait for jacktrip ports to be available before proceeding
	if ac == nil || ac.JackClient == nil {
		return
	}
	if config.Quality == 2 {
		if !ac.isValidPort("hubserver:send_1") {
			return
		}
	} else {
		if !ac.isValidPort(jamulusInputLeft) {
			return
		}
	}
	dmm.mutex.Lock()
	defer dmm.mutex.Unlock()

	// 0. fetch the fresh devices to card number mappings
	dmm.DeviceCardMapping = getDeviceToNumMappings()

	// 1. fetch the fresh capture devices list in this cycle
	activeCaptureDevices := getCaptureDeviceNames()

	// 2. get diff between the current devices list and the new devices list. Then, clean up any stale devices
	var newCaptureDevices []string
	for _, device := range activeCaptureDevices {
		if _, ok := dmm.CurrentCaptureDevices[device]; !ok {
			newCaptureDevices = append(newCaptureDevices, device)
		}
	}
	for device := range dmm.CurrentCaptureDevices {
		if !contains(activeCaptureDevices, device) {
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "a2j", device)
			StopZitaService(serviceName)
			delete(dmm.CurrentCaptureDevices, device)
		}
	}

	dmm.DeviceStream0Mapping = map[string][]string{}
	// 3. synchronize new capture devices
	for _, device := range newCaptureDevices {
		// read card num; if card num doens't exist, don't connect
		cardNum, ok := dmm.DeviceCardMapping[device]
		if !ok {
			continue
		}
		// read card stream0 using the card num
		dmm.DeviceStream0Mapping[device] = readCardStream0(cardNum)

		if err := dmm.connectZita("a2j", device, config); err == nil {
			dmm.CurrentCaptureDevices[device] = true
		}
	}

	// 4. fetch the playback capture devices list in this cycle
	activePlaybackDevices := getPlaybackDeviceNames()

	// 5. get diff between current devices and the new devices
	var newPlaybackDevices []string
	for _, device := range activePlaybackDevices {
		if _, ok := dmm.CurrentPlaybackDevices[device]; !ok {
			newPlaybackDevices = append(newPlaybackDevices, device)
		}
	}
	for device := range dmm.CurrentPlaybackDevices {
		if !contains(activePlaybackDevices, device) {
			serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "j2a", device)
			StopZitaService(serviceName)
			delete(dmm.CurrentPlaybackDevices, device)
		}
	}

	// 6. synchronize new playback devices
	for _, device := range newPlaybackDevices {
		// read card num; if card num doens't exist, don't connect
		cardNum, ok := dmm.DeviceCardMapping[device]
		if !ok {
			continue
		}

		// if device stream0 doesn't exist, read card stream0
		_, ok = dmm.DeviceStream0Mapping[device]
		if !ok {
			dmm.DeviceStream0Mapping[device] = readCardStream0(cardNum)
		}

		if err := dmm.connectZita("j2a", device, config); err == nil {
			dmm.CurrentPlaybackDevices[device] = true
		}
	}
}

func (dmm *DeviceMixingManager) connectZita(mode string, device string, config client.AgentConfig) error {
	// check if the device has support for the server sampleRate
	stream0, ok := dmm.DeviceStream0Mapping[device]
	if !ok {
		log.Info("Stream0 info does not exist", "device", device)
		return nil
	}

	channelNum := -1
	if mode == "a2j" {
		channelNum = getCaptureChannelNum(stream0, config.SampleRate)
	} else {
		channelNum = getPlaybackChannelNum(stream0, config.SampleRate)
	}

	if channelNum == -1 {
		log.Info(fmt.Sprintf("Channel num was not found for %s. Connection cannot not be established.", device))
		return nil
	}

	// write a systemd config file for Zita Bridge parameters
	if err := writeZitaConfig(channelNum, config.SampleRate, mode, device); err != nil {
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

func writeZitaConfig(numChannel int, rate int, mode string, device string) error {
	// format a path with a device and mode specific name
	connectionName := fmt.Sprintf("%s-%s", mode, device)
	path := fmt.Sprintf(PathToZitaConfig, connectionName)

	// format a config template
	zitaConfig := fmt.Sprintf(ZitaConfigTemplate, device, numChannel, rate, connectionName)
	return writeConfig(path, zitaConfig)
}

func writeConfig(path string, content string) error {
	if err := ioutil.WriteFile(path, []byte(content), 0644); err != nil {
		log.Error(err, "Error while writing config")
		return err
	}
	return nil
}

func getCaptureDeviceNames() []string {
	var names []string
	out, err := exec.Command("arecord", "-l").Output()
	if err != nil {
		log.Error(err, "Unable to retrieve capture device names")
		return names
	}

	return extractNames(string(out))
}

func getPlaybackDeviceNames() []string {
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
	var output []string
	out, err := exec.Command("cat", fmt.Sprintf("/proc/asound/card%d/stream0", cardNum)).Output()
	if err != nil {
		log.Error(err, fmt.Sprintf("Unable to retrieve card information for card %d", cardNum))
		return output
	}
	return strings.Split(string(out), "\n")
}

func getPlaybackChannelNum(sentences []string, sampleRate int) int {
	channelNum := -1
	for i, sentence := range sentences {
		// look for the playback section
		if strings.Contains(sentence, "Playback:") {
			for j := i + 1; j < len(sentences); j++ {
				currSentence := sentences[j]

				// stop if we see "Capture" which means we haven't found the right channel
				if strings.Contains(currSentence, "Capture:") {
					return channelNum
				}

				// if we found our target sampleRate, go look for the number of channels
				if strings.Contains(currSentence, "Rates:") && strings.Contains(currSentence, fmt.Sprintf("%d", sampleRate)) {
					r, _ := regexp.Compile(`Channels: (\d)`)
					for ii := j - 1; ii >= max(0, ii-4); ii-- {
						currSentence := sentences[ii]
						subMatch := r.FindStringSubmatch(currSentence)
						if len(subMatch) > 1 {
							i, err := strconv.Atoi(subMatch[1])
							if err == nil {
								channelNum = i
							}
						}
					}
				}

			}
		}
	}
	return channelNum
}

func getCaptureChannelNum(sentences []string, sampleRate int) int {
	channelNum := -1
	for i, sentence := range sentences {
		// look for the capture section
		if strings.Contains(sentence, "Capture:") {
			for j := i + 1; j < len(sentences); j++ {
				currSentence := sentences[j]

				// if we found our target sampleRate, go look for the number of channels
				if strings.Contains(currSentence, "Rates:") && strings.Contains(currSentence, fmt.Sprintf("%d", sampleRate)) {
					r, _ := regexp.Compile(`Channels: (\d)`)
					for ii := j - 1; ii >= max(0, ii-4); ii-- {
						currSentence := sentences[ii]
						subMatch := r.FindStringSubmatch(currSentence)
						if len(subMatch) > 1 {
							i, err := strconv.Atoi(subMatch[1])
							if err == nil {
								channelNum = i
							}
						}
					}
				}
			}
		}
	}
	return channelNum
}

func extractNames(target string) []string {
	var names []string
	sentences := strings.Split(target, "\n")
	r, _ := regexp.Compile(`^card \d: (\w+) \[`)
	for _, sentence := range sentences {
		subMatch := r.FindStringSubmatch(sentence)
		if len(subMatch) > 1 && subMatch[1] != "sndrpihifiberry" { // exclude hifiberry since we won't use it
			names = append(names, subMatch[1])
		}
	}
	return names
}

func extractCardNum(target string) map[string]int {
	nameToNum := map[string]int{}
	sentences := strings.Split(target, "\n")
	r, _ := regexp.Compile(`^ (\d) \[(\w+)\s*\]`)
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

func contains(lst []string, target string) bool {
	for _, item := range lst {
		if target == item {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}
