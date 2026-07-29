//go:build integration

package infrastructure

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func encodeSineToMP4(t *testing.T, path string, seconds float64) {
	t.Helper()
	session, err := OpenFDKSession(requireFDKLibrary(t), stereoSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	pcm := sinePCM(seconds, 2)
	frames, err := session.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	flushed, err := session.Flush()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewMP4AudioWriter(path, MP4AudioTrack{AudioSpecificConfig: session.AudioSpecificConfig(), Channels: 2, Bitrate: 256000, EncoderDelay: session.EncoderDelay()})
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range append(frames, flushed...) {
		if err := writer.WriteFrame(frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Finish(int64(len(pcm) / 2)); err != nil {
		t.Fatal(err)
	}
}

func TestMP4AudioWriter_FDKFramesPlayable(t *testing.T) {
	ffmpeg, _ := requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "sine.audio.m4a")

	encodeSineToMP4(t, path, 3.0)

	probed := probeFixture(t, path)
	stream := firstAudioStream(t, probed)
	duration, _ := strconv.ParseFloat(probed.Format.Duration, 64)
	if stream.CodecName != "aac" || stream.SampleRate != "44100" || stream.Channels != 2 || math.Abs(duration-3.0) > 0.02 {
		t.Fatalf("stream = %+v duration = %v", stream, duration)
	}
	if startTime := probed.Format.StartTime; startTime != "" && startTime != "0.000000" {
		t.Fatalf("start time = %s", startTime)
	}
	output, err := exec.CommandContext(context.Background(), ffmpeg, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput()
	if err != nil || len(output) > 0 {
		t.Fatalf("decode errors: %v\n%s", err, output)
	}
	if isFile(path+mp4MdatSuffix) || isFile(path+mp4ComposedSuffix) {
		t.Fatal("temporary files left behind")
	}
}

func TestMP4AudioWriter_EditListSkipsPriming(t *testing.T) {
	ffmpeg, _ := requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "sine.audio.m4a")
	encodeSineToMP4(t, path, 1.0)

	pcm, err := exec.CommandContext(context.Background(), ffmpeg, "-v", "error", "-i", path, "-ac", "1", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}

	start := 4410
	var errorEnergy, signalEnergy float64
	for index := start; index < start+441; index++ {
		decoded := float64(int16(uint16(pcm[2*index]) | uint16(pcm[2*index+1])<<8))
		expected := 8000 * math.Sin(2*math.Pi*440*float64(index)/44100)
		errorEnergy += (decoded - expected) * (decoded - expected)
		signalEnergy += expected * expected
	}
	if ratio := math.Sqrt(errorEnergy / signalEnergy); ratio > 0.1 {
		t.Fatalf("decoded audio is misaligned: relative error %.3f", ratio)
	}
}
