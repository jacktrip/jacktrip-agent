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
	CurrentCaptureDevices  []string
	CurrentPlaybackDevices []string
	// JackClient             *jack.Client
}

func (dmm *DeviceMixingManager) Reset() {
	// reinitialize device lists
	dmm.CurrentCaptureDevices = []string{}
	dmm.CurrentPlaybackDevices = []string{}

	// detect and kill any lost Zita systemd services
	cmd := `sudo systemctl --type service | grep zita | awk '/.*\.service/ {print $1}'`
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		log.Error(err, "error while looking for lost zita services")
	}

	lostServices := strings.Split(string(out), "\n")
	for _, serviceName := range lostServices {
		StopZitaService(serviceName)
	}
}

// func (dmm *DeviceMixingManager) init() bool {
// 	client, status := jack.ClientOpen("agentDeviceMixingManager", jack.NoStartServer)
// 	if status != 0 {
// 		log.Error(jack.StrError(status), "Initializing a Jack client failed")
// 		return false
// 	}
// 	dmm.JackClient = client
// 	log.Info("Initialized Jack Client in the Device Mixing Manager")
// 	return true
// }

func (dmm *DeviceMixingManager) SynchronizeConnections() {
	// 1. initialize a Go Jack client
	// if dmm.JackClient == nil {
	// 	if result := dmm.init(); result == false {
	// 		return
	// 	}
	// }

	// 2. fetch the fresh capture devices list in this cycle
	newCaptureDevices := getCaptureDeviceNames()

	// 3. get diff between the current devices list and the new devices list. Then, clean up any stale devices
	var tempCaptureDevices []string
	for _, device := range dmm.CurrentCaptureDevices {
		// if the device is current, continue
		if contains(newCaptureDevices, device) {
			tempCaptureDevices = append(tempCaptureDevices, device)
			continue
		}

		// if the device is stale, stop its systemd service and disconnect ports
		serviceName := fmt.Sprintf(ZitaA2JServiceNameTemplate, device)
		StopZitaService(serviceName)
		// dmm.disconnectZitaPorts("a2j", device)
	}
	dmm.CurrentCaptureDevices = tempCaptureDevices

	// 4. synchronize new capture devices
	for _, device := range newCaptureDevices {
		if !contains(dmm.CurrentCaptureDevices, device) {
			if err := dmm.connectZita("a2j", device); err == nil {
				dmm.CurrentCaptureDevices = append(dmm.CurrentCaptureDevices, device)
			}
		}
	}

	// 5. fetch the playback capture devices list in this cycle
	newPlaybackDevices := getPlaybackDeviceNames()

	// 6. get diff between current devices and the new devices
	var tempPlaybackDevices []string
	for _, device := range dmm.CurrentPlaybackDevices {
		// if the device is current, continue
		if contains(newPlaybackDevices, device) {
			tempPlaybackDevices = append(tempPlaybackDevices, device)
			continue
		}

		// if the device is stale, stop its systemd service and disconnect ports
		serviceName := fmt.Sprintf(ZitaJ2AServiceNameTemplate, device)
		StopZitaService(serviceName)
		// dmm.disconnectZitaPorts("j2a", device)
	}
	dmm.CurrentPlaybackDevices = tempPlaybackDevices

	// 7. synchronize new playback devices
	for _, device := range newPlaybackDevices {
		if !contains(dmm.CurrentPlaybackDevices, device) {
			if err := dmm.connectZita("j2a", device); err == nil {
				dmm.CurrentPlaybackDevices = append(dmm.CurrentPlaybackDevices, device)
			}
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
	// get port names
	// hubServerPort := "hubserver:send_1"
	// if mode == "j2a" {
	// 	hubServerPort = "hubserver:receive_1"
	// }

	// zitaPort := fmt.Sprintf("%s-%s:capture_1", mode, device)
	// if mode == "j2a" {
	// 	zitaPort = fmt.Sprintf("%s-%s:playback_1", mode, device)
	// }

	// // // write a systemd config file for Jack_connect parameters
	// // if err := writeJackConnectConfig(hubServerPort, zitaPort, mode, device); err != nil {
	// // 	log.Error(err, err.Error())
	// // 	return err
	// // }

	// /*
	// Start Jack Connect as a transient systemd-run unit
	// Note:
	// - Jack Connect command exits unlike the above Zita command. Therefore, should be treated as a transient unit
	// */
	// // if err := StartJackConnectUnit(mode, device); err != nil {
	// // 	log.Error(err, err.Error())
	// // 	return err
	// // }

	// // var out bytes.Buffer
	// // var stderr bytes.Buffer

	// time.Sleep(3 * time.Second)
	// // cmd := fmt.Sprintf(`sudo -u jacktrip jack_connect %s %s`, hubServerPort, zitaPort)
	// // out, _ := exec.Command("sh", "-c", cmd).Output()

	// if dmm.JackClient != nil {
	// 	x := dmm.JackClient.GetName()
	// 	log.Info(fmt.Sprintf("*********************** %#v", x))
	// 	time.Sleep(1 * time.Second)

	// 	y := dmm.JackClient.GetPorts("", "", 0)
	// 	log.Info(fmt.Sprintf("123123*********************** %#v", y))
	// }

	// if mode == "a2j" {
	// 	return dmm.connectPorts(zitaPort, hubServerPort)
	// }
	// return dmm.connectPorts(hubServerPort, zitaPort)

	// cmd := exec.Command("sudo", "-u", "jacktrip", "jack_connect", hubServerPort, zitaPort)
	// cmd.Stdout = &out
	// cmd.Stderr = &stderr

	// if err := cmd.Run(); err != nil {
	// 	log.Error(err, err.Error())
	// 	log.Info(stderr.String())
	// 	return err

	// log.Info(out.String())
	// log.Info(err.Error())

	// // stderr, err := cmd.StderrPipe()
	// // if err != nil {
	// // 	log.Fatal(err)
	// // }

	// err := cmd.Run()
	// if err != nil {
	//
	// 	log.Info(fmt.Sprintf("Command finished with error: %v", err))
	// }

	// // if err := cmd.Start(); err != nil {
	// // 	log.Error(err, err.Error())
	// // 	return err
	// // }

	// // slurp, _ := io.ReadAll(stderr)
	// // fmt.Printf("%s\n", slurp)

	// // if err := cmd.Wait(); err != nil {
	// // 	log.Fatal(err)
	// // }
	return nil
}

// func (dmm *DeviceMixingManager) disconnectZitaPorts(mode string, device string) error {
// 	hubServerPort := "hubserver:send_1"
// 	if mode == "j2a" {
// 		hubServerPort = "hubserver:receive_1"
// 	}

// 	zitaPort := fmt.Sprintf("%s-%s:capture_1", mode, device)
// 	if mode == "j2a" {
// 		zitaPort = fmt.Sprintf("%s-%s:playback_1", mode, device)
// 	}

// 	return dmm.disconnectPorts(hubServerPort, zitaPort)
// }

// func (dmm *DeviceMixingManager) disconnectPorts(src, dest string) error {
// 	if !dmm.isConnected(src, dest) {
// 		return nil
// 	}

// 	code := dmm.JackClient.Disconnect(src, dest)
// 	switch code {
// 	case 0:
// 		log.Info("Disconnected JACK ports", "src", src, "dest", dest)
// 	default:
// 		log.Error(jack.StrError(code), "Unexpected error disconnecting JACK ports")
// 		return jack.StrError(code)
// 	}
// 	return nil
// }

// func (dmm *DeviceMixingManager) connectPorts(src, dest string) error {
// 	if dmm.isConnected(src, dest) {
// 		return nil
// 	}

// 	code := dmm.JackClient.Connect(src, dest)
// 	switch code {
// 	case 0:
// 		log.Info("Connected JACK ports", "src", src, "dest", dest)
// 	default:
// 		log.Error(jack.StrError(code), "Unexpected error connecting JACK ports")
// 		return jack.StrError(code)
// 	}
// 	return nil
// }

// func (dmm *DeviceMixingManager) isConnected(src, dest string) bool {
// 	p := dmm.JackClient.GetPortByName(src)
// 	for _, conn := range p.GetConnections() {
// 		if conn == dest {
// 			return true
// 		}
// 	}
// 	return false
// }

func writeZitaConfig(numChannel int, rate int, mode string, device string) error {
	// format a path with a device and mode specific name
	connectionName := fmt.Sprintf("%s-%s", mode, device)
	path := fmt.Sprintf(PathToZitaConfig, connectionName)

	// format a config template
	zitaConfig := fmt.Sprintf(ZitaConfigTemplate, device, numChannel, rate, connectionName)
	return writeConfig(path, zitaConfig)
}

// func writeJackConnectConfig(port1 string, port2 string, mode string, device string) error {
// 	// format a path with a device and mode specific name
// 	path := fmt.Sprintf(PathToJackConnectConfig, fmt.Sprintf("%s-%s", mode, device))

// 	// format a config template
// 	jackConnectConfig := fmt.Sprintf(JackConnectTemplate, port1, port2)
// 	return writeConfig(path, jackConnectConfig)
// }

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

func remove(lst []string, targetIndex int) []string {
	var newList []string
	for index, element := range lst {
		if index != targetIndex {
			newList = append(newList, element)
		}
	}
	return newList
}
