package infrastructure

import (
	"context"
	"sync"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

const (
	fdkEngineName    = "FDK-AAC"
	nativeEngineName = "ffmpeg aac"
)

type OptionalAudioEncoder interface {
	Available() error
	Encode(ctx context.Context, request services.EncodeRequest) error
}

type FallbackAudioEncoder interface {
	Encode(ctx context.Context, request services.EncodeRequest) error
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

func (selector AACEncoderSelector) Encode(ctx context.Context, request services.EncodeRequest) error {
	if selector.usesFDK() {
		return selector.fdk.Encode(ctx, request)
	}
	return selector.native.Encode(ctx, request)
}

func (selector AACEncoderSelector) Profile() domain.EncodingProfile {
	if selector.usesFDK() {
		return domain.NewEncodingProfile(domain.EngineFDK)
	}
	return domain.NewEncodingProfile(domain.EngineNative)
}

func (selector AACEncoderSelector) usesFDK() bool {
	selector.choice.once.Do(func() {
		selector.choice.isFDKUsed = selector.fdk.Available() == nil
	})
	return selector.choice.isFDKUsed
}

func (selector AACEncoderSelector) EngineName() string {
	if selector.usesFDK() {
		return fdkEngineName
	}
	return nativeEngineName
}
