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

package common

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/jmoiron/sqlx/types"
	"github.com/xthexder/go-jack"
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

// Max returns the maximum of two integers
func Max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// BoolToInt converts a boolean to an integer
func BoolToInt(b types.BitBool) int {
	if b {
		return 1
	}
	return 0
}

// VolumeString returns a percentage string for volume controls
func VolumeString(vol int, mute types.BitBool) string {
	if mute {
		return "0%"
	}
	return fmt.Sprintf("%d%%", vol)
}

// InitJackClient creates a new JACK client
func InitJackClient(name string, prc jack.PortRegistrationCallback, sc jack.ShutdownCallback, pc jack.ProcessCallback, preActivationMethod func(client *jack.Client), close bool) (*jack.Client, error) {
	client, code := jack.ClientOpen(name, jack.NoStartServer)
	if client == nil || code != 0 {
		err := jack.StrError(code)
		return nil, err
	}
	// Set port registration handler
	if prc != nil {
		if code := client.SetPortRegistrationCallback(prc); code != 0 {
			err := jack.StrError(code)
			return nil, err
		}
	}
	// Set process handler
	if pc != nil {
		if code := client.SetProcessCallback(pc); code != 0 {
			err := jack.StrError(code)
			return nil, err
		}
	}
	// Set shutdown handler
	if sc != nil {
		client.OnShutdown(sc)
	}
	// Call any special routine prior to (like establishing ports)
	if preActivationMethod != nil {
		preActivationMethod(client)
	}
	if code := client.Activate(); code != 0 {
		err := jack.StrError(code)
		return nil, err
	}
	// Automatically close client upon creation - used for connection checking
	if close {
		if code := client.Close(); code != 0 {
			err := jack.StrError(code)
			return nil, err
		}
		return nil, nil
	}
	return client, nil
}

// WaitForJackd is a jack_wait reimplementation
func WaitForJackd() error {
	err := RetryWithBackoff(func() error {
		_, err := InitJackClient("", nil, nil, nil, nil, true)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
