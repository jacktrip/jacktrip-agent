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
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// JackServiceName is the name of the systemd service for Jack
	JackServiceName = "jack.service"

	// JackTripServiceName is the name of the systemd service for JackTrip
	JackTripServiceName = "jacktrip.service"

	// JamulusServiceName is the name of the systemd service for Jamulus client on RPI devices
	JamulusServiceName = "jamulus.service"

	// PathToJackConfig is the path to Jack service config file
	PathToJackConfig = "/tmp/default/jack"

	// PathToJackTripConfig is the path to JackTrip service config file
	PathToJackTripConfig = "/tmp/default/jacktrip"

	// PathToJamulusConfig is the path to Jamulus service config file
	PathToJamulusConfig = "/tmp/default/jamulus"
)

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

		receiveChannels := config.OutputChannels // audio signals from the audio server to the user, hence receiveChannels
		sendChannels := config.InputChannels     // audio signals to the audio server from user's input, hence sendChannels
		if config.Stereo {
			if receiveChannels == 0 {
				receiveChannels = 2 // default output channels is stereo
			}
			if sendChannels == 0 {
				sendChannels = 1 // default input channels is mono
			}
		} else {
			receiveChannels = 1
			sendChannels = 1
		}

		jackTripConfig = fmt.Sprintf(JackTripDeviceConfigTemplate, receiveChannels, sendChannels, config.Host, config.Port, config.DevicePort, remoteName, strings.TrimSpace(jackTripExtraOpts))
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
	units, err := conn.ListUnitsByNames([]string{JackServiceName,
		SCSynthServiceName, SupernovaServiceName, SCLangServiceName, JackAutoconnectServiceName,
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
			servicesToStart = append(servicesToStart, SCSynthServiceName, SCLangServiceName, JackAutoconnectServiceName)
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
			servicesToStart = append(servicesToStart, SCSynthServiceName, SCLangServiceName, JackAutoconnectServiceName)
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
		return fmt.Errorf("failed to stop %s: job status=%s", u.Name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("failed to stop %s: job status=%s", u.Name, jobStatus)
	}

	return nil
}

// startService is used to start a managed systemd service
func startService(conn *dbus.Conn, name string) error {
	log.Info("Starting managed service", "name", name)

	reschan := make(chan string)
	_, err := conn.StartUnit(name, "replace", reschan)

	if err != nil {
		return fmt.Errorf("failed to start %s: job status=%s", name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("failed to start %s: job status=%s", name, jobStatus)
	}
	log.Info("Finished starting managed service", "name", name)
	return nil
}

// startService is used to restart a managed systemd service
func restartService(name string) error {
	log.Info("Restarting managed service", "name", name)

	// create dbus connection to manage systemd units
	conn, err := dbus.New()
	if err != nil {
		return errors.New("failed to connect to dbus")
	}
	defer conn.Close()

	reschan := make(chan string)
	_, err = conn.RestartUnit(name, "replace", reschan)
	if err != nil {
		return fmt.Errorf("failed to restart %s: job status=%s", name, err.Error())
	}

	jobStatus := <-reschan
	if jobStatus != "done" {
		return fmt.Errorf("failed to restart %s: job status=%s", name, jobStatus)
	}
	log.Info("Finished restarting managed service", "name", name)
	return nil
}
