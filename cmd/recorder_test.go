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
	"testing"

	"github.com/grafov/m3u8"
	"github.com/stretchr/testify/assert"
)

func TestBpsToInt(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(uint32(192000), bpsToInt("192k"))
	assert.Equal(uint32(211000), bpsToInt("211k"))
	assert.Equal(uint32(320000), bpsToInt("320k"))
	assert.Equal(uint32(1411000), bpsToInt("1411k"))
	assert.Equal(uint32(0), bpsToInt("abcdek"))
	assert.Equal(uint32(0), bpsToInt("k"))
}

func TestConstructPrimaryPlaylist(t *testing.T) {
	assert := assert.New(t)

	m1 := m3u8.NewMasterPlaylist()
	assert.Equal(0, len(m1.Variants))

	constructPrimaryPlaylist(m1)
	assert.Equal(uint8(7), m1.Version())
	assert.Equal(3, len(m1.Variants))
	m1String := m1.String()
	assert.Contains(m1String, "#EXTM3U")
	assert.Contains(m1String, "#EXT-X-VERSION:7")
	assert.Contains(m1String, "#EXT-X-INDEPENDENT-SEGMENTS")
	assert.Contains(m1String, `#EXT-X-STREAM-INF:PROGRAM-ID=0,BANDWIDTH=211000,AVERAGE-BANDWIDTH=192000,CODECS="mp4a.40.2"`)
	assert.Contains(m1String, `#EXT-X-STREAM-INF:PROGRAM-ID=0,BANDWIDTH=352000,AVERAGE-BANDWIDTH=320000,CODECS="mp4a.40.2"`)
	assert.Contains(m1String, `#EXT-X-STREAM-INF:PROGRAM-ID=0,BANDWIDTH=1536000,AVERAGE-BANDWIDTH=1411000`)
}

func TestConstructTranscodingArgs(t *testing.T) {
	assert := assert.New(t)

	HLSPlaylistHash = "abcd1234"
	args1 := constructTranscodingArgs("/home/bmanilow/copacabana.flac", "aac", "320k")
	expected1 := []string{
		"-hide_banner", "-i", "/home/bmanilow/copacabana.flac",
		"-c:a", "aac", "-b:a", "320k",
		"-f", "hls", "-hls_segment_type", "fmp4",
		"-strict", "experimental",
		"-hls_time", "6",
		"-hls_list_size", "5",
		"-hls_flags", "delete_segments+append_list+round_durations+omit_endlist+program_date_time",
		"-hls_fmp4_init_filename", "init-abcd1234-320k.mp4",
		"-hls_segment_filename", "/tmp/vs-media/copacabana-320k-%03d.m4s",
		"/tmp/vs-media/hiddenstream-320k.m3u8",
	}
	assert.Equal(expected1, args1)

	HLSPlaylistHash = "xyz8990"
	args2 := constructTranscodingArgs("/home/bmanilow/let-the-bodies-hit-the-floor.flac", "flac", "1411k")
	expected2 := []string{
		"-hide_banner", "-i", "/home/bmanilow/let-the-bodies-hit-the-floor.flac",
		"-c:a", "flac", "-b:a", "1411k",
		"-f", "hls", "-hls_segment_type", "fmp4",
		"-strict", "experimental",
		"-hls_time", "6",
		"-hls_list_size", "5",
		"-hls_flags", "delete_segments+append_list+round_durations+omit_endlist+program_date_time",
		"-hls_fmp4_init_filename", "init-xyz8990-1411k.mp4",
		"-hls_segment_filename", "/tmp/vs-media/let-the-bodies-hit-the-floor-1411k-%03d.m4s",
		"/tmp/vs-media/hiddenstream-1411k.m3u8",
	}
	assert.Equal(expected2, args2)
}

func TestInsertNewMedia(t *testing.T) {
	assert := assert.New(t)
	var m1String string

	HLSPlaylistHash = "xyz890"
	m1, _ := m3u8.NewMediaPlaylist(3, 3)

	HLSIndex = 0
	insertNewMedia(m1, "/home/bmanilow/copacabana.flac", "192k")
	// At this point, there should be 3 segments but the last 2 are nil
	assert.Equal(3, len(m1.Segments))
	assert.NotNil(m1.Segments[0])
	assert.Nil(m1.Segments[1])
	assert.Nil(m1.Segments[2])
	assert.Equal("copacabana-192k-000.m4s", m1.Segments[0].URI)
	assert.Equal(5.0, m1.Segments[0].Duration)
	assert.Equal(true, m1.Segments[0].Discontinuity)
	m1String = m1.String()
	assert.Contains(m1String, "#EXT-X-MEDIA-SEQUENCE:0")
	assert.NotContains(m1String, "#EXT-X-DISCONTINUITY-SEQUENCE")
	assert.Contains(m1String, "#EXT-X-TARGETDURATION:5")
	assert.Contains(m1String, "#EXT-X-DISCONTINUITY")
	assert.Contains(m1String, `#EXT-X-MAP:URI="init-xyz890-192k.mp4"`)
	assert.Contains(m1String, "#EXTINF:5.000,")
	assert.Contains(m1String, "copacabana-192k-000.m4s")

	HLSIndex = 1
	insertNewMedia(m1, "/home/bmanilow/mandy.flac", "320k")
	// At this point, there should be 3 segments but the last 1 is nil; first segment is preserved
	assert.Equal(3, len(m1.Segments))
	assert.NotNil(m1.Segments[0])
	assert.NotNil(m1.Segments[1])
	assert.Nil(m1.Segments[2])
	assert.Equal("copacabana-192k-000.m4s", m1.Segments[0].URI)
	assert.Equal("mandy-320k-001.m4s", m1.Segments[1].URI)
	assert.Equal(5.0, m1.Segments[0].Duration)
	assert.Equal(true, m1.Segments[0].Discontinuity)
	assert.Equal(5.0, m1.Segments[1].Duration)
	assert.Equal(true, m1.Segments[1].Discontinuity)
	m1String = m1.String()
	assert.Contains(m1String, "#EXT-X-MEDIA-SEQUENCE:0")
	assert.NotContains(m1String, "#EXT-X-DISCONTINUITY-SEQUENCE")
	assert.Contains(m1String, "#EXT-X-TARGETDURATION:5")
	assert.Contains(m1String, "#EXT-X-DISCONTINUITY")
	assert.Contains(m1String, `#EXT-X-MAP:URI="init-xyz890-192k.mp4"`)
	assert.Contains(m1String, "#EXTINF:5.000,")
	assert.Contains(m1String, "copacabana-192k-000.m4s")
	assert.Contains(m1String, "mandy-320k-001.m4s")

	HLSIndex = 2
	insertNewMedia(m1, "/home/bmanilow/i-write-the-songs.flac", "1500k")
	// At this point, there should be 3 active segments
	assert.Equal(3, len(m1.Segments))
	assert.NotNil(m1.Segments[0])
	assert.NotNil(m1.Segments[1])
	assert.NotNil(m1.Segments[2])
	assert.Equal("copacabana-192k-000.m4s", m1.Segments[0].URI)
	assert.Equal("mandy-320k-001.m4s", m1.Segments[1].URI)
	assert.Equal("i-write-the-songs-1500k-002.m4s", m1.Segments[2].URI)
	assert.Equal(5.0, m1.Segments[0].Duration)
	assert.Equal(true, m1.Segments[0].Discontinuity)
	assert.Equal(5.0, m1.Segments[1].Duration)
	assert.Equal(true, m1.Segments[1].Discontinuity)
	assert.Equal(5.0, m1.Segments[2].Duration)
	assert.Equal(true, m1.Segments[2].Discontinuity)
	m1String = m1.String()
	assert.Contains(m1String, "#EXT-X-MEDIA-SEQUENCE:0")
	assert.NotContains(m1String, "#EXT-X-DISCONTINUITY-SEQUENCE")
	assert.Contains(m1String, "#EXT-X-TARGETDURATION:5")
	assert.Contains(m1String, "#EXT-X-DISCONTINUITY")
	assert.Contains(m1String, `#EXT-X-MAP:URI="init-xyz890-192k.mp4"`)
	assert.Contains(m1String, "#EXTINF:5.000,")
	assert.Contains(m1String, "copacabana-192k-000.m4s")
	assert.Contains(m1String, "mandy-320k-001.m4s")
	assert.Contains(m1String, "i-write-the-songs-1500k-002.m4s")

	HLSIndex = 3
	insertNewMedia(m1, "/home/bmanilow/somewhereinthenight.flac", "1411k")
	// At this point, there should be 3 active segments but the first one should be evicted
	// Also EXT-X-DISCONTINUITY-SEQUENCE appears and EXT-X-MEDIA-SEQUENCE is incremented
	assert.Equal(3, len(m1.Segments))
	assert.NotNil(m1.Segments[0])
	assert.NotNil(m1.Segments[1])
	assert.NotNil(m1.Segments[2])
	assert.Equal("somewhereinthenight-1411k-003.m4s", m1.Segments[0].URI)
	assert.Equal("mandy-320k-001.m4s", m1.Segments[1].URI)
	assert.Equal("i-write-the-songs-1500k-002.m4s", m1.Segments[2].URI)
	assert.Equal(5.0, m1.Segments[0].Duration)
	assert.Equal(true, m1.Segments[0].Discontinuity)
	assert.Equal(5.0, m1.Segments[1].Duration)
	assert.Equal(true, m1.Segments[1].Discontinuity)
	assert.Equal(5.0, m1.Segments[2].Duration)
	assert.Equal(true, m1.Segments[2].Discontinuity)
	m1String = m1.String()
	assert.Contains(m1String, "#EXT-X-MEDIA-SEQUENCE:1")
	assert.Contains(m1String, "#EXT-X-DISCONTINUITY-SEQUENCE:1")
	assert.Contains(m1String, "#EXT-X-TARGETDURATION:5")
	assert.Contains(m1String, "#EXT-X-DISCONTINUITY")
	assert.Contains(m1String, `#EXT-X-MAP:URI="init-xyz890-192k.mp4"`)
	assert.Contains(m1String, "#EXTINF:5.000,")
	assert.NotContains(m1String, "copacabana-192k-000.m4s")
	assert.Contains(m1String, "somewhereinthenight-1411k-003.m4s")
	assert.Contains(m1String, "mandy-320k-001.m4s")
	assert.Contains(m1String, "i-write-the-songs-1500k-002.m4s")
}
