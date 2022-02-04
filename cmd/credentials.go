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
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

const (
	// SecretBytes are used to generate random secret strings
	SecretBytes = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

func init() {
	// seed random number generator for secret generation
	rand.Seed(time.Now().UnixNano())
}

// getCredentials retrieves jacktrip agent credentials from system config file.
// If config does not exist, it will generate and save new credentials to config file.
func getCredentials() client.AgentCredentials {
	var rawBytes []byte
	var err error

	rawBytes = []byte(os.Getenv("JACKTRIP_API_SECRET"))
	if len(rawBytes) == 0 {
		rawBytes, err = ioutil.ReadFile(fmt.Sprintf("%s/credentials", AgentConfigDir))
		if err != nil {
			log.Error(err, "Failed to read credentials")
			panic(err)
		}
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
