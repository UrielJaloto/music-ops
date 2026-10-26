package infrastructure

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/UrielJaloto/music-ops/services"
)

const partialSuffix = ".tmp"

type PCMDecoder interface {
	Stream(ctx context.Context, request DecodeRequest, sink io.Writer) error
}

type fdkLibraryState struct {
	once    sync.Once
	library FDKLibrary
	err     error
}

type FDKAACEncoder struct {
	decoder     PCMDecoder
	libraryPath string
	state       *fdkLibraryState
}

func NewFDKAACEncoder(decoder PCMDecoder, libraryPath string) FDKAACEncoder {
	return FDKAACEncoder{decoder: decoder, libraryPath: libraryPath, state: &fdkLibraryState{}}
}

func (encoder FDKAACEncoder) Available() error {
	encoder.state.once.Do(func() {
		encoder.state.library, encoder.state.err = LoadFDKLibrary(encoder.libraryPath)
	})
	return encoder.state.err
}

func (encoder FDKAACEncoder) Version() (string, error) {
	if err := encoder.Available(); err != nil {
		return "", err
	}
	return encoder.state.library.Version()
}

func (encoder FDKAACEncoder) Encode(ctx context.Context, request services.EncodeRequest) error {
	if err := encoder.Available(); err != nil {
		return err
	}
	partial := request.Destination + partialSuffix
	if err := encoder.encodeTo(ctx, request, partial); err != nil {
		return errors.Join(err, removeIfPresent(partial))
	}
	if err := os.Rename(partial, request.Destination); err != nil {
		return errors.Join(fmt.Errorf("renaming encoded file: %w", err), removeIfPresent(partial))
	}
	return nil
}

func (encoder FDKAACEncoder) encodeTo(ctx context.Context, request services.EncodeRequest, path string) error {
	channels := request.Profile.OutputChannels(request.Quality)
	session, err := OpenFDKSession(encoder.state.library, FDKSettings{
		SampleRate: request.Profile.SampleRate(), Channels: channels,
		Bitrate: request.Profile.Bitrate(), Bandwidth: request.Profile.CutoffHz(),
	})
	if err != nil {
		return err
	}
	defer session.Close()
	writer, err := NewMP4AudioWriter(path, MP4AudioTrack{
		AudioSpecificConfig: session.AudioSpecificConfig(), Channels: channels,
		Bitrate: request.Profile.Bitrate(), EncoderDelay: session.EncoderDelay(),
	})
	if err != nil {
		return err
	}
	pipeline := fdkPipeline{session: session, writer: writer, channels: channels}
	if err := pipeline.run(ctx, encoder.decoder, DecodeRequest{Source: request.Source, Profile: request.Profile, Quality: request.Quality}); err != nil {
		return errors.Join(err, writer.Abort())
	}
	return nil
}

type fdkPipeline struct {
	session       *FDKSession
	writer        *MP4AudioWriter
	channels      int
	sourceSamples int64
}

func (pipeline *fdkPipeline) run(ctx context.Context, decoder PCMDecoder, request DecodeRequest) error {
	reader, sink := io.Pipe()
	decoded := make(chan error, 1)
	go func() {
		err := decoder.Stream(ctx, request, sink)
		sink.CloseWithError(err)
		decoded <- err
	}()
	encodeErr := pipeline.consume(reader)
	_ = reader.CloseWithError(errPipelineStopped)
	if decodeErr := <-decoded; decodeErr != nil {
		return fmt.Errorf("decoding %s: %w", request.Source, decodeErr)
	}
	if encodeErr != nil {
		return encodeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return pipeline.finish()
}

var errPipelineStopped = errors.New("encoding pipeline stopped")

func (pipeline *fdkPipeline) consume(reader io.Reader) error {
	chunk := make([]byte, pipeline.session.FrameLength()*pipeline.channels*pcmSampleBytes)
	for {
		read, err := io.ReadFull(reader, chunk)
		if encodeErr := pipeline.encode(chunk[:read-read%(pcmSampleBytes*pipeline.channels)]); encodeErr != nil {
			return encodeErr
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (pipeline *fdkPipeline) encode(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	pcm := make([]int16, len(chunk)/pcmSampleBytes)
	for index := range pcm {
		pcm[index] = int16(binary.LittleEndian.Uint16(chunk[index*pcmSampleBytes:]))
	}
	pipeline.sourceSamples += int64(len(pcm) / pipeline.channels)
	frames, err := pipeline.session.Encode(pcm)
	if err != nil {
		return err
	}
	return pipeline.writeFrames(frames)
}

func (pipeline *fdkPipeline) finish() error {
	frames, err := pipeline.session.Flush()
	if err != nil {
		return err
	}
	if err := pipeline.writeFrames(frames); err != nil {
		return err
	}
	return pipeline.writer.Finish(pipeline.sourceSamples)
}

func (pipeline *fdkPipeline) writeFrames(frames [][]byte) error {
	for _, frame := range frames {
		if err := pipeline.writer.WriteFrame(frame); err != nil {
			return err
		}
	}
	return nil
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
