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
	"flag"
	"fmt"
	"os"
)

const (
	// AgentConfigDir is the directory containing agent config files
	AgentConfigDir = "/etc/jacktrip"

	// AgentLibDir is the directory containing additional files used by the agent
	AgentLibDir = "/var/lib/jacktrip"
)

// GitSHA is the commit hash of the current build
var GitSHA string

// main wires everything together and starts up the Agent server
func main() {
	apiOrigin := flag.String("o", "https://app.jacktrip.org/api", "origin to use when constructing API endpoints")
	version := flag.Bool("v", false, "display version and exit")
	flag.Parse()

	if *version {
		fmt.Printf("Git SHA: %s\n", GitSHA)
		return
	}

	// require this be run as root
	if os.Geteuid() != 0 {
		log.Info("jacktrip-agent must be run as root")
		os.Exit(1)
	}

	runOnDevice(*apiOrigin)
	log.Info("Exiting")
}
