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
	"bufio"
	"encoding/base64"
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
func updateServiceConfigs(config client.DeviceAgentConfig, remoteName string) {

	// assume auto queue unless > 0
	jackTripExtraOpts := fmt.Sprintf("--bufstrategy %d", config.BufferStrategy)
	if config.QueueBuffer > 0 {
		jackTripExtraOpts = fmt.Sprintf("%s -q %d", jackTripExtraOpts, config.QueueBuffer)
	} else {
		jackTripExtraOpts = fmt.Sprintf("%s -q auto", jackTripExtraOpts)
	}

	// create config opts from templates
	var jackConfig, jackTripConfig string

	updateJamulusIni(config, remoteName)

	jackConfig = fmt.Sprintf(JackDeviceConfigTemplate, "alsa -d hw:"+soundDeviceName, config.SampleRate, config.Period)
	if soundDeviceName == "dummy" {
		jackConfig = fmt.Sprintf(JackDeviceConfigTemplate, soundDeviceName, config.SampleRate, config.Period)
	}

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
	if receiveChannels == 0 {
		receiveChannels = 2 // default output channels is stereo
	}
	if sendChannels == 0 {
		sendChannels = 1 // default input channels is mono
	}

	jackTripConfig = fmt.Sprintf(JackTripDeviceConfigTemplate, receiveChannels, sendChannels, config.Host, config.Port, config.DevicePort, remoteName, strings.TrimSpace(jackTripExtraOpts))

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
}

// updateJamulusIni writes a new /tmp/jamulus.ini file using template at /var/lib/jacktrip/jamulus.ini
func updateJamulusIni(config client.DeviceAgentConfig, remoteName string) {
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
	nameRx := regexp.MustCompile(`.*<name_base64>.*</name_base64>.*`)

	writeToFile := func() {
		line := scanner.Text()
		if audioQualityRx.MatchString(line) {
			line = fmt.Sprintf(" <audioquality>%d</audioquality>", quality)
		}
		if nameRx.MatchString(line) {
			line = fmt.Sprintf(" <name_base64>%s</name_base64>", base64.StdEncoding.EncodeToString([]byte(remoteName)))
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

// StartZitaService starts a zita service
func StartZitaService(serviceName string) error {
	conn, err := dbus.New()
	if err != nil {
		log.Error(err, "Failed to connect to dbus")
	}
	defer conn.Close()

	err = startService(conn, serviceName)
	if err != nil {
		log.Error(err, "Unable to start service", "name", serviceName)
	}
	return err
}

// StopZitaService stops a running zita service
func StopZitaService(serviceName string) error {
	conn, err := dbus.New()
	if err != nil {
		log.Error(err, "Failed to connect to dbus")
		return err
	}
	defer conn.Close()

	// stop any managed services that are active
	units, err := conn.ListUnitsByNames([]string{serviceName})
	if err != nil {
		log.Error(err, "Failed to get status of managed services")
		return err
	}

	for _, u := range units {
		err = stopService(conn, u)
		if err != nil {
			log.Error(err, "Unable to stop service")
			return err
		}
	}
	return nil
}

// restartAllServices is used to restart all of the managed systemd services
func restartAllServices(config client.DeviceAgentConfig) {
	// create dbus connection to manage systemd units
	conn, err := dbus.New()
	if err != nil {
		log.Error(err, "Failed to connect to dbus")
		panic(err)
	}
	defer conn.Close()

	// stop any managed services that are active
	units, err := conn.ListUnitsByNames([]string{JackServiceName, JackTripServiceName, JamulusServiceName})
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
	case client.Jamulus:
		servicesToStart = []string{JackServiceName, JamulusServiceName}
	case client.JackTripJamulus:
		switch config.Quality {
		case 0:
			servicesToStart = []string{JackServiceName, JamulusServiceName}
		case 1:
			servicesToStart = []string{JackServiceName, JamulusServiceName}
		case 2:
			servicesToStart = []string{JackServiceName, JackTripServiceName}
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

	log.Info("Finished stopping managed service", "name", u.Name)
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

// killService is used to kill a managed systemd service
func killService(name string) {
	conn, err := dbus.New()
	if err != nil {
		log.Error(err, "Failed to connect to dbus")
		return
	}
	defer conn.Close()
	log.Info("Killing managed service", "name", name)
	conn.KillUnit(name, 9)
}
