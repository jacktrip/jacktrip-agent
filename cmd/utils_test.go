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
	"testing"

	"github.com/jmoiron/sqlx/types"
	"github.com/stretchr/testify/assert"
)

func TestMax(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(1, max(1, 1))
	assert.Equal(3, max(1, 3))
	assert.Equal(3, max(3, 1))
	assert.Equal(5, max(5, 2))
	assert.Equal(5, max(2, 5))
	assert.Equal(1, max(0, 1))
	assert.Equal(1, max(1, 0))
}

func TestBoolToInt(t *testing.T) {
	assert := assert.New(t)

	// explicitly check types.BitBool input
	yes := types.BitBool(true)
	assert.Equal(1, boolToInt(yes))
	no := types.BitBool(false)
	assert.Equal(0, boolToInt(no))
	// explicitly check bool input
	assert.Equal(1, boolToInt(true))
	assert.Equal(0, boolToInt(false))
}

func TestVolumeString(t *testing.T) {
	assert := assert.New(t)

	yes := types.BitBool(true)
	assert.Equal("0%", volumeString(10, yes))
	assert.Equal("0%", volumeString(0, yes))

	no := types.BitBool(false)
	assert.Equal("30%", volumeString(30, no))
	assert.Equal("100%", volumeString(100, no))
}
