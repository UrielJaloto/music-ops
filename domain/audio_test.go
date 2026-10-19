package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewCodec(t *testing.T) {
	cases := []struct {
		name          string
		raw           string
		expected      Codec
		expectedError error
	}{
		{name: "Empty_ReturnsErrCodecMissing", raw: "", expectedError: ErrCodecMissing},
		{name: "Blank_ReturnsErrCodecMissing", raw: "   ", expectedError: ErrCodecMissing},
		{name: "Uppercase_IsLowered", raw: "FLAC", expected: "flac"},
		{name: "Padded_IsTrimmed", raw: " aac ", expected: "aac"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			codec, err := NewCodec(testCase.raw)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if codec != testCase.expected || codec.String() != string(testCase.expected) {
				t.Fatalf("codec = %q, want %q", codec, testCase.expected)
			}
		})
	}
}

func TestCodecFamily(t *testing.T) {
	cases := []struct {
		codec    Codec
		expected AudioFamily
	}{
		{"flac", FamilyLossless},
		{"alac", FamilyLossless},
		{"ape", FamilyLossless},
		{"wavpack", FamilyLossless},
		{"tta", FamilyLossless},
		{"truehd", FamilyLossless},
		{"mlp", FamilyLossless},
		{"pcm_s24le", FamilyLossless},
		{"pcm_s16le", FamilyLossless},
		{"aac", FamilyAAC},
		{"mp3", FamilyLossyOther},
		{"opus", FamilyLossyOther},
		{"vorbis", FamilyLossyOther},
		{"unknown", FamilyLossyOther},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.codec), func(t *testing.T) {
			if got := testCase.codec.Family(); got != testCase.expected {
				t.Fatalf("Family = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func validQualityDetails() AudioQualityDetails {
	return AudioQualityDetails{SampleRate: 44100, BitDepth: 16, Channels: 2, BitRate: 1411000, Duration: time.Second}
}

func qualityWith(change func(*AudioQualityDetails)) AudioQualityDetails {
	details := validQualityDetails()
	change(&details)
	return details
}

func TestNewAudioQuality(t *testing.T) {
	cases := []struct {
		name          string
		details       AudioQualityDetails
		expectedError error
	}{
		{"SampleRate7999_Rejected", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 7999 }), ErrSampleRateOutOfRange},
		{"SampleRate8000_Accepted", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 8000 }), nil},
		{"SampleRate8001_Accepted", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 8001 }), nil},
		{"SampleRate767999_Accepted", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 767999 }), nil},
		{"SampleRate768000_Accepted", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 768000 }), nil},
		{"SampleRate768001_Rejected", qualityWith(func(d *AudioQualityDetails) { d.SampleRate = 768001 }), ErrSampleRateOutOfRange},
		{"Channels0_Rejected", qualityWith(func(d *AudioQualityDetails) { d.Channels = 0 }), ErrChannelsOutOfRange},
		{"Channels1_Accepted", qualityWith(func(d *AudioQualityDetails) { d.Channels = 1 }), nil},
		{"Channels2_Accepted", qualityWith(func(d *AudioQualityDetails) { d.Channels = 2 }), nil},
		{"Channels31_Accepted", qualityWith(func(d *AudioQualityDetails) { d.Channels = 31 }), nil},
		{"Channels32_Accepted", qualityWith(func(d *AudioQualityDetails) { d.Channels = 32 }), nil},
		{"Channels33_Rejected", qualityWith(func(d *AudioQualityDetails) { d.Channels = 33 }), ErrChannelsOutOfRange},
		{"Duration0_Rejected", qualityWith(func(d *AudioQualityDetails) { d.Duration = 0 }), ErrDurationMissing},
		{"DurationNegative_Rejected", qualityWith(func(d *AudioQualityDetails) { d.Duration = -1 }), ErrDurationMissing},
		{"Duration1ns_Accepted", qualityWith(func(d *AudioQualityDetails) { d.Duration = 1 }), nil},
		{"BitDepth0_Accepted", qualityWith(func(d *AudioQualityDetails) { d.BitDepth = 0 }), nil},
		{"BitDepth7_Rejected", qualityWith(func(d *AudioQualityDetails) { d.BitDepth = 7 }), ErrBitDepthOutOfRange},
		{"BitDepth8_Accepted", qualityWith(func(d *AudioQualityDetails) { d.BitDepth = 8 }), nil},
		{"BitDepth64_Accepted", qualityWith(func(d *AudioQualityDetails) { d.BitDepth = 64 }), nil},
		{"BitDepth65_Rejected", qualityWith(func(d *AudioQualityDetails) { d.BitDepth = 65 }), ErrBitDepthOutOfRange},
		{"BitRateNegative_Rejected", qualityWith(func(d *AudioQualityDetails) { d.BitRate = -1 }), ErrBitRateNegative},
		{"BitRate0_Accepted", qualityWith(func(d *AudioQualityDetails) { d.BitRate = 0 }), nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			quality, err := NewAudioQuality(testCase.details)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err != nil {
				return
			}
			assertQualityMatches(t, quality, testCase.details)
		})
	}
}

func assertQualityMatches(t *testing.T, quality AudioQuality, details AudioQualityDetails) {
	t.Helper()
	got := AudioQualityDetails{quality.SampleRate(), quality.BitDepth(), quality.Channels(), quality.BitRate(), quality.Duration()}
	if got != details {
		t.Fatalf("quality = %+v, want %+v", got, details)
	}
}

func mustQuality(t *testing.T, change func(*AudioQualityDetails)) AudioQuality {
	t.Helper()
	quality, err := NewAudioQuality(qualityWith(change))
	if err != nil {
		t.Fatalf("building quality: %v", err)
	}
	return quality
}

func TestEncodingProfile_Constants(t *testing.T) {
	cases := []struct {
		name            string
		engine          EncoderEngine
		expectedBitrate int
	}{
		{name: "AUD3_FDKBitrate", engine: EngineFDK, expectedBitrate: 256000},
		{name: "AUD3_NativeBitrate", engine: EngineNative, expectedBitrate: 512000},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			profile := NewEncodingProfile(testCase.engine)

			if profile.Bitrate() != testCase.expectedBitrate || profile.SampleRate() != 44100 || profile.CutoffHz() != 20000 {
				t.Fatalf("profile = %d/%d/%d", profile.Bitrate(), profile.SampleRate(), profile.CutoffHz())
			}
		})
	}
}

func TestEncodingProfile_OutputChannels(t *testing.T) {
	cases := []struct{ source, expected int }{{1, 1}, {2, 2}, {3, 2}, {6, 2}}
	for _, testCase := range cases {
		quality := mustQuality(t, func(d *AudioQualityDetails) { d.Channels = testCase.source })

		if got := NewEncodingProfile(EngineFDK).OutputChannels(quality); got != testCase.expected {
			t.Errorf("AUD6 channels %d -> %d, want %d", testCase.source, got, testCase.expected)
		}
	}
}

func TestEncodingProfile_NeedsResampling(t *testing.T) {
	cases := []struct {
		sampleRate int
		expected   bool
	}{{44100, false}, {48000, true}, {96000, true}, {22050, true}}
	for _, testCase := range cases {
		quality := mustQuality(t, func(d *AudioQualityDetails) { d.SampleRate = testCase.sampleRate })

		if got := NewEncodingProfile(EngineFDK).NeedsResampling(quality); got != testCase.expected {
			t.Errorf("AUD5 %d -> %v, want %v", testCase.sampleRate, got, testCase.expected)
		}
	}
}

func TestDecideEncoding(t *testing.T) {
	cases := []struct {
		name     string
		codec    Codec
		expected EncodingDecision
	}{
		{name: "AUD8_AACIsCopied", codec: "aac", expected: DecisionCopy},
		{name: "AUD9_MP3IsEncoded", codec: "mp3", expected: DecisionEncode},
		{name: "AUD9_OpusIsEncoded", codec: "opus", expected: DecisionEncode},
		{name: "LosslessIsEncoded", codec: "flac", expected: DecisionEncode},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := DecideEncoding(testCase.codec); got != testCase.expected {
				t.Fatalf("DecideEncoding = %v, want %v", got, testCase.expected)
			}
		})
	}
}
