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
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/xthexder/go-jack"
	"sync"
	"testing"
)

func TestConsts(t *testing.T) {
	assert := assert.New(t)
	assert.Equal(2, defaultChannels)
	assert.Equal(`:(send|receive)_`, jacktripPortToken)
	assert.Equal("SuperCollider:in_", supercolliderInput)
	assert.Equal("SuperCollider:out_", supercolliderOutput)
	assert.Equal("supernova:input_", supernovaInput)
	assert.Equal("supernova:output_", supernovaOutput)
	assert.Equal("Jamulus:input left", jamulusInputLeft)
	assert.Equal("Jamulus:input right", jamulusInputRight)
	assert.Equal("Jamulus:output left", jamulusOutputLeft)
	assert.Equal("Jamulus:output right", jamulusOutputRight)
}

func TestNewAutoConnector(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	assert.Equal("*main.AutoConnector", fmt.Sprintf("%T", ac))
	assert.Equal("autoconnector", ac.Name)
	assert.Equal(2, ac.Channels)
	assert.Equal(false, ac.FullScanDone)
	assert.Equal(1, len(ac.KnownClients))
	assert.Equal(0, ac.KnownClients["Jamulus"])
}

func TestHandlePortRegistration(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	// Check that no messages appear when register=false
	ac.handlePortRegistration(jack.PortId(0), false)
	select {
	case x := <-ac.RegistrationChan:
		assert.Fail(fmt.Sprintf("no value should be read but got: %v", x))
	default:
	}
	// Check that a message appears when register=true
	ac.handlePortRegistration(jack.PortId(1), true)

	x := <-ac.RegistrationChan
	assert.Equal(jack.PortId(1), x)
}

func TestGetClientNum(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	// Verify the internal map gets updated
	assert.Equal(1, ac.getClientNum("a"))
	assert.Equal(2, ac.getClientNum("b"))
	assert.Equal(3, ac.getClientNum("c"))
	assert.Equal(1, ac.getClientNum("a"))
	assert.Equal(4, len(ac.KnownClients))
	assert.Equal(0, ac.KnownClients["Jamulus"])
	assert.Equal(1, ac.KnownClients["a"])
	assert.Equal(2, ac.KnownClients["b"])
	assert.Equal(3, ac.KnownClients["c"])
}

func TestGetServerChannel(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	// Verify the server channel numbers are correct and the internal map is updated
	assert.Equal(4, ac.getServerChannel("dusty", 2))
	assert.Equal(5, ac.getServerChannel("dusty", 3))
	assert.Equal(5, ac.getServerChannel("not-dusty", 1))
	assert.Equal(3, len(ac.KnownClients))
	assert.Equal(0, ac.KnownClients["Jamulus"])
	assert.Equal(1, ac.KnownClients["dusty"])
	assert.Equal(2, ac.KnownClients["not-dusty"])
}

func TestOnShutdown(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	// onShutdown should revert the FullScanDone boolean
	ac.FullScanDone = true
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ac.onShutdown()
		wg.Done()
	}()
	wg.Wait()
	assert.False(ac.FullScanDone)
	assert.Nil(ac.JackClient)
	x := <-ac.RegistrationChan
	assert.Equal(jack.PortId(0), x)
}

func TestTeardownClient(t *testing.T) {
	assert := assert.New(t)
	ac := NewAutoConnector()
	// onShutdown should revert the FullScanDone boolean
	ac.FullScanDone = true
	ac.TeardownClient()
	assert.False(ac.FullScanDone)
	assert.Nil(ac.JackClient)
}
