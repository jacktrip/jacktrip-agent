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

package jackutils

import (
	"github.com/jacktrip/jacktrip-agent/pkg/common"
	"github.com/xthexder/go-jack"
)

// InitJackClient creates a new JACK client
func InitJackClient(name string, prc jack.PortRegistrationCallback, sc jack.ShutdownCallback, pc jack.ProcessCallback, xc jack.XRunCallback, preActivationMethod func(client *jack.Client), close bool) (*jack.Client, error) {
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
	// Set xrun handler
	if xc != nil {
		if code := client.SetXRunCallback(xc); code != 0 {
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
	err := common.RetryWithBackoff(func() error {
		_, err := InitJackClient("", nil, nil, nil, nil, nil, true)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
