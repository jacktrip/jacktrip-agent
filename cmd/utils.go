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
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/jmoiron/sqlx/types"
)

const (
	// RetryMaxAttempts sets the maximum number of attempts when retrying
	RetryMaxAttempts = 10

	// RetryBackoffFactor sets the exponential backoff factor on wait duration
	RetryBackoffFactor = 2

	// RetryBackoffMax sets the maximum wait duration between retry attempts
	RetryBackoffMax = 10000 // milliseconds
)

func exponentialBackoffSleep(iteration int) {
	desired := int(math.Pow(float64(iteration+1), float64(RetryBackoffFactor)))
	actual := RetryBackoffMax
	if desired*1000 < RetryBackoffMax {
		actual = desired * 1000
	}
	jitter := rand.Intn(1000)
	time.Sleep(time.Duration(actual+jitter) * time.Millisecond)
}

// RetryWithBackoff implements a retry-loop with an expontential backoff algorithm
func RetryWithBackoff(run func() error) error {
	for i := 0; i < RetryMaxAttempts; i++ {
		err := run()
		if err != nil {
			if i < RetryMaxAttempts-1 {
				exponentialBackoffSleep(i)
				continue
			}
			return err
		}
		break
	}
	return nil
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// boolToInt converts a boolean to an integer
func boolToInt(b types.BitBool) int {
	if b {
		return 1
	}
	return 0
}

// volumeString returns a percentage string for volume controls
func volumeString(vol int, mute types.BitBool) string {
	if mute {
		return "0%"
	}
	return fmt.Sprintf("%d%%", vol)
}
