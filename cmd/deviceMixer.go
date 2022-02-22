package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strings"
	// "bytes"
)

const (
	PathToZitaConfig        = "/tmp/default/zita-%s-conf"
	PathToJackConnectConfig = "/tmp/default/jack-connect-%s-conf"
	ZitaConfigTemplate      = "ZITA_OPTS=-d hw:%s -c %d -r %d -j %s\n"
	// JackConnectTemplate = "JACK_CONNECT_OPTS=%s %s\n"
	ZitaA2JServiceNameTemplate = "zita-a2j-@%s.service"
	ZitaJ2AServiceNameTemplate = "zita-j2a-@%s.service"
	ZitaServiceNameTemplate    = "zita-%s-@%s.service"
)

type DeviceMixingManager struct {
	CurrentCaptureDevices  map[string]bool
	CurrentPlaybackDevices map[string]bool
	// JackClient             *jack.Client
}

func (dmm *DeviceMixingManager) Reset() {
	for device := range dmm.CurrentCaptureDevices {
		serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "a2j", device)
		StopZitaService(serviceName)
	}

	for device := range dmm.CurrentPlaybackDevices {
		serviceName := fmt.Sprintf(ZitaServiceNameTemplate, "j2a", device)
		StopZitaService(serviceName)
	}

	// reinitialize device lists
	dmm.CurrentCaptureDevices = map[string]bool{}
	dmm.CurrentPlaybackDevices = map[string]bool{}

	// detect and kill any lost Zita systemd services
	/*
		cmd := `sudo systemctl --type service | grep zita | awk '/.*\.service/ {print $1}'`
		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			log.Error(err, "error while looking for lost zita services")
		}

		lostServices := strings.Split(string(out), "\n")
		for _, serviceName := range lostServices {
			StopZitaService(serviceName)
		}
	*/
}

// TODO: We should probably have error handling here
func (dmm *DeviceMixingManager) SynchronizeConnections() {
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

	// 3. synchronize new capture devices
	for _, device := range newCaptureDevices {
		if err := dmm.connectZita("a2j", device); err == nil {
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
		if err := dmm.connectZita("j2a", device); err == nil {
			dmm.CurrentPlaybackDevices[device] = true

		}
	}
}

func (dmm *DeviceMixingManager) connectZita(mode string, device string) error {
	// write a systemd config file for Zita Bridge parameters
	if err := writeZitaConfig(1, 48000, mode, device); err != nil {
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

	names = extractNames(string(out))
	return names
}

func getPlaybackDeviceNames() []string {
	var names []string
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		log.Error(err, "Unable to retrieve playback device names")
		return nil
	}

	names = extractNames(string(out))
	return names
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

func contains(lst []string, target string) bool {
	for _, item := range lst {
		if target == item {
			return true
		}
	}
	return false
}
