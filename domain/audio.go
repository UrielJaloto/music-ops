package domain

import (
	"strings"
	"time"
)

var (
	ErrCodecMissing         = newBusinessError("audio codec is missing")
	ErrSampleRateOutOfRange = newBusinessError("sample rate out of range")
	ErrChannelsOutOfRange   = newBusinessError("channel count out of range")
	ErrDurationMissing      = newBusinessError("audio duration is missing")
	ErrBitDepthOutOfRange   = newBusinessError("bit depth out of range")
	ErrBitRateNegative      = newBusinessError("bit rate is negative")
)

const (
	minimumSampleRate     = 8000
	maximumSampleRate     = 768000
	minimumChannels       = 1
	maximumChannels       = 32
	minimumBitDepth       = 8
	maximumBitDepth       = 64
	targetSampleRate      = 44100
	cutoffHz              = 20000
	maximumOutputChannels = 2
	fdkBitrate            = 256000
	nativeBitrate         = 512000
	pcmCodecPrefix        = "pcm_"
)

type Codec string

type AudioFamily int

const (
	FamilyLossless AudioFamily = iota
	FamilyAAC
	FamilyLossyOther
)

var codecFamilies = map[Codec]AudioFamily{
	"flac":    FamilyLossless,
	"alac":    FamilyLossless,
	"ape":     FamilyLossless,
	"wavpack": FamilyLossless,
	"tta":     FamilyLossless,
	"truehd":  FamilyLossless,
	"mlp":     FamilyLossless,
	"aac":     FamilyAAC,
}

func NewCodec(name string) (Codec, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", ErrCodecMissing
	}
	return Codec(normalized), nil
}

func (codec Codec) Family() AudioFamily {
	if strings.HasPrefix(string(codec), pcmCodecPrefix) {
		return FamilyLossless
	}
	family, isListed := codecFamilies[codec]
	if !isListed {
		return FamilyLossyOther
	}
	return family
}

func (codec Codec) String() string {
	return string(codec)
}

type AudioQualityDetails struct {
	SampleRate int
	BitDepth   int
	Channels   int
	BitRate    int
	Duration   time.Duration
}

type AudioQuality struct {
	sampleRate int
	bitDepth   int
	channels   int
	bitRate    int
	duration   time.Duration
}

func NewAudioQuality(details AudioQualityDetails) (AudioQuality, error) {
	if err := validateAudioQuality(details); err != nil {
		return AudioQuality{}, err
	}
	return AudioQuality{
		sampleRate: details.SampleRate,
		bitDepth:   details.BitDepth,
		channels:   details.Channels,
		bitRate:    details.BitRate,
		duration:   details.Duration,
	}, nil
}

func (quality AudioQuality) SampleRate() int {
	return quality.sampleRate
}

func (quality AudioQuality) BitDepth() int {
	return quality.bitDepth
}

func (quality AudioQuality) Channels() int {
	return quality.channels
}

func (quality AudioQuality) BitRate() int {
	return quality.bitRate
}

func (quality AudioQuality) Duration() time.Duration {
	return quality.duration
}

func validateAudioQuality(details AudioQualityDetails) error {
	if !isWithin(details.SampleRate, minimumSampleRate, maximumSampleRate) {
		return ErrSampleRateOutOfRange
	}
	if !isWithin(details.Channels, minimumChannels, maximumChannels) {
		return ErrChannelsOutOfRange
	}
	if details.Duration <= 0 {
		return ErrDurationMissing
	}
	if !isValidBitDepth(details.BitDepth) {
		return ErrBitDepthOutOfRange
	}
	if details.BitRate < 0 {
		return ErrBitRateNegative
	}
	return nil
}

func isWithin(value, minimum, maximum int) bool {
	return value >= minimum && value <= maximum
}

func isValidBitDepth(bitDepth int) bool {
	return bitDepth == 0 || isWithin(bitDepth, minimumBitDepth, maximumBitDepth)
}

type EncoderEngine int

const (
	EngineFDK EncoderEngine = iota
	EngineNative
)

var constantBitrateEngines = map[EncoderEngine]bool{
	EngineFDK:    true,
	EngineNative: false,
}

var engineBitrates = map[EncoderEngine]int{
	EngineFDK:    fdkBitrate,
	EngineNative: nativeBitrate,
}

type EncodingProfile struct {
	bitrate           int
	isConstantBitrate bool
}

func NewEncodingProfile(engine EncoderEngine) EncodingProfile {
	return EncodingProfile{bitrate: engineBitrates[engine], isConstantBitrate: constantBitrateEngines[engine]}
}

func (profile EncodingProfile) HasConstantBitrate() bool {
	return profile.isConstantBitrate
}

func (profile EncodingProfile) Bitrate() int {
	return profile.bitrate
}

func (profile EncodingProfile) SampleRate() int {
	return targetSampleRate
}

func (profile EncodingProfile) CutoffHz() int {
	return cutoffHz
}

func (profile EncodingProfile) OutputChannels(source AudioQuality) int {
	return min(source.Channels(), maximumOutputChannels)
}

func (profile EncodingProfile) NeedsResampling(source AudioQuality) bool {
	return source.SampleRate() != targetSampleRate
}

type EncodingDecision int

const (
	DecisionEncode EncodingDecision = iota
	DecisionCopy
)

func DecideEncoding(codec Codec) EncodingDecision {
	if codec.Family() == FamilyAAC {
		return DecisionCopy
	}
	return DecisionEncode
}
