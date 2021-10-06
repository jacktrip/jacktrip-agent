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
	"encoding/json"
	"os"

	"github.com/jacktrip/jacktrip-agent/pkg/client"
)

// collectMetrics uses jacktrip-agent to collect JACK and connection metrics
func collectMetrics() {
	type MetricsResponse struct {
		Metrics []client.ServerMetric `json:"metrics"`
	}
	response := MetricsResponse{}
	ac = NewAutoConnector()
	response.Metrics = ac.CollectMetrics()
	json.NewEncoder(os.Stdout).Encode(response)
}
