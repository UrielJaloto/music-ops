//go:build integration

package transcoding

import (
	"math"
	"os"
	"testing"
)

func requireFDKLibrary(t *testing.T) FDKLibrary {
	t.Helper()
	library, err := LoadFDKLibrary(fdkLibraryPath())
	if err == nil {
		return library
	}
	if strict, _ := os.LookupEnv("MUSIC_OPS_STRICT"); strict == "1" {
		t.Fatalf("FDK-AAC library is required: %v", err)
	}
	t.Skipf("FDK-AAC library is not installed: %v", err)
	return FDKLibrary{}
}

func stereoSettings() FDKSettings {
	return FDKSettings{SampleRate: 44100, Channels: 2, Bitrate: 256000, Bandwidth: 20000}
}

func sinePCM(seconds float64, channels int) []int16 {
	frames := int(seconds * 44100)
	pcm := make([]int16, 0, frames*channels)
	for index := range frames {
		value := int16(8000 * math.Sin(2*math.Pi*440*float64(index)/44100))
		for range channels {
			pcm = append(pcm, value)
		}
	}
	return pcm
}
