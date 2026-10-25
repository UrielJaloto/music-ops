//go:build integration

package infrastructure

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

const defaultFDKLibraryPath = `C:\Program Files\freac\codecs\FDK-AAC.dll`

func TestFDKStructLayouts_MatchHeader(t *testing.T) {
	cases := []struct {
		name     string
		size     uintptr
		expected uintptr
	}{
		{name: "AACENC_BufDesc", size: unsafe.Sizeof(aacBufferDescriptor{}), expected: 40},
		{name: "AACENC_InArgs", size: unsafe.Sizeof(aacInputArguments{}), expected: 8},
		{name: "AACENC_OutArgs", size: unsafe.Sizeof(aacOutputArguments{}), expected: 16},
		{name: "AACENC_InfoStruct", size: unsafe.Sizeof(aacInfo{}), expected: 96},
		{name: "LIB_INFO", size: unsafe.Sizeof(aacLibraryInfo{}), expected: 72},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.size != testCase.expected {
				t.Fatalf("size = %d, want %d", testCase.size, testCase.expected)
			}
		})
	}
}

func TestLoadFDKLibrary_MissingPath_ReturnsErrFDKUnavailable(t *testing.T) {
	_, err := LoadFDKLibrary(filepath.Join(t.TempDir(), "FDK-AAC.dll"))

	if !errors.Is(err, ErrFDKUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFDKLibrary_NotADLL_ReturnsErrFDKUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FDK-AAC.dll")
	writeTestFile(t, path, "not a library")

	_, err := LoadFDKLibrary(path)

	if !errors.Is(err, ErrFDKUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestFDKError_Error(t *testing.T) {
	message := FDKError{Code: 0x23, Operation: "aacEncEncode"}.Error()

	if message != "aacEncEncode failed with FDK-AAC error 0x23" {
		t.Fatalf("message = %q", message)
	}
}

func requireFDKLibrary(t *testing.T) FDKLibrary {
	t.Helper()
	path, isSet := os.LookupEnv("MUSIC_OPS_ENCODER_FDK_LIBRARY")
	if !isSet || path == "" {
		path = defaultFDKLibraryPath
	}
	library, err := LoadFDKLibrary(path)
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

func TestFDKSession_EncodeStereoSine(t *testing.T) {
	library := requireFDKLibrary(t)
	session, err := OpenFDKSession(library, stereoSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	pcm := sinePCM(2, 2)

	frames, err := session.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	flushed, err := session.Flush()
	if err != nil {
		t.Fatal(err)
	}

	expected := int(math.Ceil(float64(len(pcm)/2+session.EncoderDelay()) / 1024))
	total := len(frames) + len(flushed)
	if total < expected-1 || total > expected+1 {
		t.Fatalf("frames = %d, want %d ± 1 (delay %d)", total, expected, session.EncoderDelay())
	}
	if session.FrameLength() != 1024 || session.EncoderDelay() <= 0 {
		t.Fatalf("frame length = %d delay = %d", session.FrameLength(), session.EncoderDelay())
	}
}

func TestFDKSession_AudioSpecificConfig(t *testing.T) {
	library := requireFDKLibrary(t)
	session, err := OpenFDKSession(library, stereoSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	config := session.AudioSpecificConfig()

	if len(config) != 2 || config[0] != 0x12 || config[1] != 0x10 {
		t.Fatalf("asc = % x", config)
	}
}

func TestFDKSession_SettingLimits(t *testing.T) {
	library := requireFDKLibrary(t)
	cases := []struct {
		name     string
		bitrate  int
		channels int
		isValid  bool
	}{
		{name: "BitrateBelowMinimum", bitrate: 7999, channels: 2},
		{name: "BitrateAtMinimum", bitrate: 8000, channels: 2, isValid: true},
		{name: "MonoAt256k", bitrate: 256000, channels: 1, isValid: true},
		{name: "MonoAtMaximum", bitrate: 264600, channels: 1, isValid: true},
		{name: "MonoAboveMaximum", bitrate: 264601, channels: 1},
		{name: "StereoAtMaximum", bitrate: 529200, channels: 2, isValid: true},
		{name: "StereoAboveMaximum", bitrate: 529201, channels: 2},
		{name: "ZeroChannels", bitrate: 256000, channels: 0},
		{name: "ThreeChannels", bitrate: 256000, channels: 3},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := stereoSettings()
			settings.Bitrate = testCase.bitrate
			settings.Channels = testCase.channels

			session, err := OpenFDKSession(library, settings)

			var fdkError FDKError
			if testCase.isValid && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !testCase.isValid && (!errors.As(err, &fdkError) || fdkError.Code != 0x23) {
				t.Fatalf("err = %v", err)
			}
			if session != nil {
				session.Close()
			}
		})
	}
}

func TestFDKSession_CloseTwiceIsSafe(t *testing.T) {
	library := requireFDKLibrary(t)
	session, err := OpenFDKSession(library, stereoSettings())
	if err != nil {
		t.Fatal(err)
	}

	session.Close()
	session.Close()
}

func TestFDKLibrary_Version(t *testing.T) {
	library := requireFDKLibrary(t)

	version, err := library.Version()

	if err != nil || version == "" {
		t.Fatalf("version = %q err = %v", version, err)
	}
}
