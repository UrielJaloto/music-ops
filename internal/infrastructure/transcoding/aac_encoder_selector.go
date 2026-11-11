package transcoding

import (
	"context"
	"strconv"
	"sync"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
)

type OptionalAudioEncoder interface {
	Available() error
	Encode(ctx context.Context, request conversion.EncodeRequest) error
}

type FallbackAudioEncoder interface {
	Encode(ctx context.Context, request conversion.EncodeRequest) error
}

type encoderChoice struct {
	once      sync.Once
	isFDKUsed bool
}

type AACEncoderSelector struct {
	fdk    OptionalAudioEncoder
	native FallbackAudioEncoder
	choice *encoderChoice
}

func NewAACEncoderSelector(fdk OptionalAudioEncoder, native FallbackAudioEncoder) AACEncoderSelector {
	return AACEncoderSelector{fdk: fdk, native: native, choice: &encoderChoice{}}
}

func (selector AACEncoderSelector) Encode(ctx context.Context, request conversion.EncodeRequest) error {
	if selector.usesFDK() {
		return selector.fdk.Encode(ctx, request)
	}
	return selector.native.Encode(ctx, request)
}

func (selector AACEncoderSelector) SelectProfile() audio.EncodingProfile {
	if selector.usesFDK() {
		return audio.NewEncodingProfile(audio.EngineFDK)
	}
	return audio.NewEncodingProfile(audio.EngineNative)
}

func (selector AACEncoderSelector) usesFDK() bool {
	selector.choice.once.Do(func() {
		selector.choice.isFDKUsed = selector.fdk.Available() == nil
	})
	return selector.choice.isFDKUsed
}

func parseIntOrZero(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}
