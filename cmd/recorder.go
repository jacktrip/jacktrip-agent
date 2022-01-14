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
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafov/m3u8"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
	"github.com/xthexder/go-jack"
)

const (
	// FileDuration is the duration (in seconds) of each audio segment file
	FileDuration = 5
	// FileCountLimit is the maximum number of audio segment files kept on disk
	FileCountLimit = 10
	// NumRecorderChannels is the number of input channels of the recorder
	NumRecorderChannels = 2
	// BitDepth is the bit-resolution used when encoding audio data - changing this involves changes in processBuffer()
	BitDepth = 16
	// HLSWindowSize is the number of URIs in the m3u8 sliding window (HLSWindowSize < FileCountLimit due to rotation)
	HLSWindowSize = 5
	// PlaylistNameTmpl is the pattern used when writing m3u8 playlists to disk
	playlistNameTmpl = "playlist-%s-%s.m3u8"
)

var (
	// AudioFilenames is an in-memory array of filenames used to perform rotation
	AudioFilenames []string
	// FrameBuffer is an in-memory buffer of FLAC frames
	FrameBuffer []frame.Frame
	// HLSMasterPlaylist is the top-level HLS playlist struct
	HLSMasterPlaylist *m3u8.MasterPlaylist
	// HLSMediaPlaylists is an array of HLS media playlists
	HLSMediaPlaylists []*m3u8.MediaPlaylist
	// HLSIndex is the top-level HLS sequence counter
	HLSIndex = 0
	// HLSPlaylistHash is a randomly generated hash to uniquely identify playlists on restart of jackd/jacktrip
	HLSPlaylistHash string
	// HLSSupportedOutputs are the output target formats
	HLSSupportedOutputs = []map[string]string{
		// 1. max bps is roughly 110% of the average bps
		// 2. 192k AAC required: https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices
		// 3. est. ~1411k FLAC for lossless: https://www.gearpatrol.com/tech/audio/a36585957/lossless-audio-explained/
		{
			"avgBps": "192k",
			"maxBps": "211k",
			"codec":  "aac",
		},
		{
			"avgBps": "320k",
			"maxBps": "352k",
			"codec":  "aac",
		},
		{
			"avgBps": "1411k",
			"maxBps": "1536k",
			"codec":  "flac",
		},
	}
)

// Recorder listens to audio and records it to disk
type Recorder struct {
	// Name will be the JACK client name
	Name string
	// JackBufferSize is the buffer size determined at the start of client initiation
	JackBufferSize int
	// JackSampleRate is the sample rate determined at the start of client initiation
	JackSampleRate int
	// JackClient is the client interacting with jackd
	JackClient *jack.Client
	// RecorderPorts are the input ports used to scrape audio
	RecorderPorts []*jack.Port
	// ClientLock is a mutex used for client-daemon interactions
	ClientLock sync.Mutex
	// RawSamplesChan is the channel were raw audio samples are passed through
	RawSamplesChan chan [][]jack.AudioSample
}

// NewRecorder constructs a new instance of Recorder
func NewRecorder() *Recorder {
	return &Recorder{
		Name:           "recorder",
		RawSamplesChan: make(chan [][]jack.AudioSample, 500),
	}
}

// processBuffer reads frames from each port's buffer
func (r *Recorder) processBuffer(nframes uint32) int {
	// Ignore raw audio samples if client initiation is still in progress
	if r.JackBufferSize <= 0 || r.JackSampleRate <= 0 {
		return 0
	}
	// Read audio data from ports and immediately send to the channel for further processing
	raw := [][]jack.AudioSample{}
	for _, port := range r.RecorderPorts {
		samples := port.GetBuffer(nframes)
		raw = append(raw, samples)
	}
	r.RawSamplesChan <- raw
	return 0
}

// reset nullifies all things recording-related in a thread-unsafe manner - used when jackd/jacktrip restarts
func (r *Recorder) reset() {
	r.JackClient, r.RecorderPorts, r.JackSampleRate, r.JackBufferSize = nil, nil, 0, 0
	for _, trash := range AudioFilenames {
		basename := filepath.Base(trash)
		basenameWithoutExt := strings.TrimSuffix(basename, filepath.Ext(basename))
		cleanFiles(filepath.Join(MediaDir, fmt.Sprintf("%s*", basenameWithoutExt)))
	}
	cleanFiles(filepath.Join(MediaDir, "*.m3u8"))
	cleanFiles(filepath.Join(MediaDir, "*.mp4"))
	cleanFiles(filepath.Join(MediaDir, "*.ts"))
	cleanFiles(filepath.Join(MediaDir, "*.m4s"))
	AudioFilenames, FrameBuffer = nil, nil
	HLSMasterPlaylist = nil
	log.Info("Teardown of recorder completed")
}

// onShutdown only runs upon unexpected connection error
func (r *Recorder) onShutdown() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	r.reset()
}

// TeardownClient closes the currently active JACK client
func (r *Recorder) TeardownClient() {
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	if r.JackClient != nil {
		r.JackClient.Close()
	}
	r.reset()
}

// SetupClient establishes a new client to listen in on JACK ports
func (r *Recorder) SetupClient() {
	var err error
	r.ClientLock.Lock()
	defer r.ClientLock.Unlock()
	err = waitForDaemon()
	if err != nil {
		panic(err)
	}
	portRegistrationFunc := func(client *jack.Client) {
		for i := 1; i <= NumRecorderChannels; i++ {
			portName := fmt.Sprintf("send_%d", i)
			portIn := client.PortRegister(portName, jack.DEFAULT_AUDIO_TYPE, jack.PortIsInput, 0)
			r.RecorderPorts = append(r.RecorderPorts, portIn)
		}
	}
	client, err := initClient(r.Name, nil, r.onShutdown, r.processBuffer, portRegistrationFunc, false)
	if err != nil {
		panic(err)
	}
	r.JackClient = client
	r.JackSampleRate = int(r.JackClient.GetSampleRate())
	r.JackBufferSize = int(r.JackClient.GetBufferSize())
	HLSMasterPlaylist = m3u8.NewMasterPlaylist()
	HLSMasterPlaylist.SetVersion(7)
	log.Info("Setup of JACK client completed", "name", r.JackClient.GetName())
}

// Run is the primary loop that is connects new JACK ports upon registration
func (r *Recorder) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		raw, ok := <-r.RawSamplesChan
		if !ok {
			log.Info("Raw samples channel is closed")
			return
		}
		r.addFrame(raw)
	}
}

func (r *Recorder) addFrame(audioSamples [][]jack.AudioSample) {
	sampleRate, bufferSize := r.JackSampleRate, r.JackBufferSize
	if sampleRate <= 0 || bufferSize <= 0 {
		return
	}
	if len(FrameBuffer) >= sampleRate*FileDuration/bufferSize {
		go flush(FrameBuffer, sampleRate)
		FrameBuffer = []frame.Frame{}
	}
	subframes := make([]*frame.Subframe, NumRecorderChannels)
	for i := range subframes {
		subframe := &frame.Subframe{
			Samples: make([]int32, bufferSize),
		}
		subframes[i] = subframe
	}
	// Dump raw sample data into respective subframes
	for i, samples := range audioSamples {
		subHdr := frame.SubHeader{
			Pred:   frame.PredVerbatim,
			Order:  0,
			Wasted: 0,
		}
		subframes[i].SubHeader = subHdr
		subframes[i].NSamples = bufferSize
		subframes[i].Samples = subframes[i].Samples[:bufferSize]
		for j, sample := range samples {
			subframes[i].Samples[j] = int32(uint16(sample * math.MaxInt16))
		}
	}
	// Package subframes into frame using stereo settings
	header := frame.Header{
		HasFixedBlockSize: false,
		BlockSize:         uint16(bufferSize),
		SampleRate:        uint32(sampleRate),
		Channels:          frame.ChannelsLR,
		BitsPerSample:     BitDepth,
	}
	frame := &frame.Frame{Header: header, Subframes: subframes}
	FrameBuffer = append(FrameBuffer, *frame)
}

// createNewFLAC creates a new FLAC file based on server ID and current time
func createNewFLAC() (*os.File, error) {
	name := fmt.Sprintf("%s/raw-%s-%d.flac", MediaDir, os.Getenv("JACKTRIP_SERVER_ID"), time.Now().Unix())
	fh, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return fh, nil
}

// openFLAC creates a FLAC encoder to write audio samples to a file
func openFLAC(sampleRate int) (*flac.Encoder, error) {
	fh, err := createNewFLAC()
	if err != nil {
		return nil, err
	}
	// Keep track of files created and rotate files
	AudioFilenames = append(AudioFilenames, fh.Name())
	if len(AudioFilenames) > FileCountLimit {
		basename := filepath.Base(AudioFilenames[0])
		basenameWithoutExt := strings.TrimSuffix(basename, filepath.Ext(basename))
		cleanFiles(filepath.Join(MediaDir, fmt.Sprintf("%s*", basenameWithoutExt)))
		AudioFilenames = AudioFilenames[1:]
	}
	info := &meta.StreamInfo{
		BlockSizeMin:  16,
		BlockSizeMax:  65535,
		SampleRate:    uint32(sampleRate),
		NChannels:     NumRecorderChannels,
		BitsPerSample: BitDepth,
	}
	return flac.NewEncoder(fh, info)
}

// bpsToInt turns a bitrate string to an int
func bpsToInt(bitrate string) uint32 {
	b := strings.Split(bitrate, "k")[0]
	result, err := strconv.Atoi(b)
	if err != nil {
		return 0
	}
	return uint32(result * 1000)
}

// constructPrimaryPlaylist idempotently configures the top-level playlist for a new set of streams
func constructPrimaryPlaylist(playlist *m3u8.MasterPlaylist) {
	playlist.Variants = nil
	playlist.SetVersion(7)
	playlist.SetIndependentSegments(true)
	// Reset global counter, playlist array, and sub-playlist identifier
	HLSIndex = 0
	HLSMediaPlaylists = nil
	HLSPlaylistHash = generateRandomString(8)
	for _, out := range HLSSupportedOutputs {
		// Create a new media playlist for each bitrate
		stream, err := m3u8.NewMediaPlaylist(HLSWindowSize, HLSWindowSize)
		if err != nil {
			panic(err)
		}
		stream.SetVersion(7)
		name := fmt.Sprintf(playlistNameTmpl, HLSPlaylistHash, out["avgBps"])
		HLSMediaPlaylists = append(HLSMediaPlaylists, stream)
		// Add to master playlist
		codec := ""
		if out["codec"] == "aac" {
			codec = "mp4a.40.2"
		}
		playlist.Append(name, nil, m3u8.VariantParams{AverageBandwidth: bpsToInt(out["avgBps"]), Bandwidth: bpsToInt(out["maxBps"]), Codecs: codec})
	}
}

// constructTranscodingArgs builds the ffmpeg arguments used for transcoding
func constructTranscodingArgs(sampleFilepath string) []string {
	fname := getFilename(sampleFilepath)
	// Call ffmpeg using the sampleFile input, ex: `ffmpeg -hide_banner -i raw.flac`
	ffmpegArgs := []string{"-hide_banner", "-i", sampleFilepath}
	for i, out := range HLSSupportedOutputs {
		// Target output encoding + bitrate, ex: `-map 0:a -c:a:0 aac -b:a:0 192k`
		dest := []string{
			"-map", "0:a",
			fmt.Sprintf("-c:a:%d", i),
			out["codec"],
			fmt.Sprintf("-b:a:%d", i),
			out["avgBps"],
		}
		ffmpegArgs = append(ffmpegArgs, dest...)
	}
	// Add HLS-specific output options
	ffmpegArgs = append(ffmpegArgs, []string{
		// Transcode to HLS-compatible fragmented MP4 files
		"-f", "hls", "-hls_segment_type", "fmp4",
		"-hls_time", strconv.Itoa(FileDuration + 1),
		"-hls_list_size", strconv.Itoa(HLSWindowSize),
		"-hls_flags", "delete_segments+append_list+round_durations+omit_endlist+program_date_time",
		"-hls_fmp4_init_filename", fmt.Sprintf("playlist-%s-%%v-init.mp4", HLSPlaylistHash),
		"-hls_segment_filename", fmt.Sprintf("%s/%s-%%v-%%03d.m4s", MediaDir, fname),
		// Enable experimental flags for flac->fmp4
		"-strict", "experimental",
		// These playlist names are required but unused because we need to craft the manifest ourselves
		"-master_pl_name", "hiddenstream-index.m3u8",
		"-var_stream_map", "a:0 a:1", filepath.Join(MediaDir, "hiddenstream-%v.m3u8"),
	}...)
	return ffmpegArgs
}

// insertNewMedia pushes a new media segment to a designated playlist
func insertNewMedia(plist *m3u8.MediaPlaylist, sampleFilepath string, index int) {
	fname := getFilename(sampleFilepath)
	uri := fmt.Sprintf("%s-%d-%03d.m4s", fname, index, HLSIndex)
	// When the playlist has reached capacity, manually shift some metadata to account for changes
	if plist.Count() == 0 {
		initMP4 := fmt.Sprintf("playlist-%s-%d-init.mp4", HLSPlaylistHash, index)
		plist.SetDefaultMap(initMP4, 0, 0)
	}
	// Insert new segment into playlist; add discontinuity because technically it's a distinct file
	plist.Slide(uri, FileDuration, "")
	plist.SetDiscontinuity()
	plist.SetProgramDateTime(time.Now().Truncate(time.Second))
	// Keep #EXT-X-DISCONTINUITY-SEQUENCE in sync with #EXT-X-MEDIA-SEQUENCE
	plist.DiscontinuitySeq = plist.SeqNo
}

func updateHLSPlaylist() {
	if HLSMasterPlaylist == nil {
		return
	}
	// Add variants to the main playlist once after init
	if len(HLSMasterPlaylist.Variants) != len(HLSSupportedOutputs) {
		constructPrimaryPlaylist(HLSMasterPlaylist)
		os.WriteFile(fmt.Sprintf("%s/index.m3u8", MediaDir), HLSMasterPlaylist.Encode().Bytes(), 0644)
	}
	// Transcode raw sample into proper HLS-compatible containers
	newSample := AudioFilenames[len(AudioFilenames)-1]
	ffmpegArgs := constructTranscodingArgs(newSample)
	cmd := exec.Command("ffmpeg", ffmpegArgs...)
	cmd.CombinedOutput()
	// Update each media playlist with the newest segment
	for i, out := range HLSSupportedOutputs {
		plist := HLSMediaPlaylists[i]
		insertNewMedia(plist, newSample, i)
		plistFilename := fmt.Sprintf(playlistNameTmpl, HLSPlaylistHash, out["avgBps"])
		os.WriteFile(filepath.Join(MediaDir, plistFilename), plist.Encode().Bytes(), 0644)
	}
	// Increment global counter for tracking purposes
	HLSIndex++
}

func flush(frameBuffer []frame.Frame, sampleRate int) {
	if len(frameBuffer) <= 0 {
		return
	}
	encoder, err := openFLAC(sampleRate)
	if err != nil {
		log.Error(err, "Failed to create FLAC encoder")
		return
	}
	defer encoder.Close()
	for _, frame := range frameBuffer {
		if err := encoder.WriteFrame(&frame); err != nil {
			log.Error(err, "Failed to write FLAC frame")
			return
		}
	}
	updateHLSPlaylist()
}
