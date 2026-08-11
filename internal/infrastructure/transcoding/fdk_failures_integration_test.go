//go:build integration && windows

package transcoding

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	failingProcedure = kernel32.NewProc("GetCurrentProcessId")
	idleProcedure    = kernel32.NewProc("IsDebuggerPresent")
)

type sineDecoder struct{}

func (sineDecoder) Stream(_ context.Context, _ DecodeRequest, sink io.Writer) error {
	return binary.Write(sink, binary.LittleEndian, sinePCM(0.5, 2))
}

type stubbornDecoder struct{}

func (stubbornDecoder) Stream(ctx context.Context, request DecodeRequest, sink io.Writer) error {
	_ = sineDecoder{}.Stream(ctx, request, sink)
	return nil
}

func stereoEncodeRequest(t *testing.T, destination string) conversion.EncodeRequest {
	t.Helper()
	quality, err := audio.NewAudioQuality(audio.AudioQualityDetails{SampleRate: 44100, Channels: 2, Duration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return conversion.EncodeRequest{Source: "sine", Destination: destination, Profile: audio.NewEncodingProfile(audio.EngineFDK), Quality: quality}
}

func TestLoadFDKLibrary_DLLWithoutEncoder_ReturnsErrFDKUnavailable(t *testing.T) {
	system, _ := os.LookupEnv("SystemRoot")

	_, err := LoadFDKLibrary(filepath.Join(system, "System32", "kernel32.dll"))

	if !errors.Is(err, ErrFDKUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenFDKSession_LibraryFailures(t *testing.T) {
	cases := []struct {
		name      string
		swap      func(library *FDKLibrary)
		operation string
	}{
		{name: "Open", swap: func(library *FDKLibrary) { library.open = failingProcedure }, operation: "aacEncOpen"},
		{name: "SetParam", swap: func(library *FDKLibrary) { library.setParam = failingProcedure }, operation: "aacEncoder_SetParam(0x0100)"},
		{name: "Initialize", swap: func(library *FDKLibrary) { library.encode = failingProcedure }, operation: "aacEncEncode(init)"},
		{name: "Info", swap: func(library *FDKLibrary) { library.info = failingProcedure }, operation: "aacEncInfo"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			library := requireFDKLibrary(t)
			testCase.swap(&library)

			_, err := OpenFDKSession(library, stereoSettings())

			var fdkError FDKError
			if !errors.As(err, &fdkError) || fdkError.Operation != testCase.operation {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func openTestSession(t *testing.T) *FDKSession {
	t.Helper()
	session, err := OpenFDKSession(requireFDKLibrary(t), stereoSettings())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}

func TestFDKSession_EncodeFailures(t *testing.T) {
	failing := openTestSession(t)
	failing.library.encode = failingProcedure
	idle := openTestSession(t)
	idle.library.encode = idleProcedure

	_, encodeErr := failing.Encode(sinePCM(0.1, 2))
	_, flushErr := failing.Flush()
	frames, idleErr := idle.Encode(sinePCM(0.1, 2))

	var fdkError FDKError
	if !errors.As(encodeErr, &fdkError) || !errors.As(flushErr, &fdkError) || idleErr != nil || len(frames) != 0 {
		t.Fatalf("errors = %v %v %v frames = %d", encodeErr, flushErr, idleErr, len(frames))
	}
}

func TestFDKAACEncoder_Encode_FileSystemFailures(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(t *testing.T, folder string) string
		expected string
	}{
		{name: "DestinationIsAFolder", prepare: func(t *testing.T, folder string) string {
			destination := filepath.Join(folder, "out.m4a")
			writeTestFile(t, filepath.Join(destination, "inside"), "x")
			return destination
		}, expected: "renaming encoded file"},
		{name: "FolderMissing", prepare: func(_ *testing.T, folder string) string {
			return filepath.Join(folder, "missing", "out.m4a")
		}, expected: "creating media data"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			requireFDKLibrary(t)
			folder := t.TempDir()
			destination := testCase.prepare(t, folder)

			err := NewFDKAACEncoder(sineDecoder{}, fdkLibraryPath()).Encode(context.Background(), stereoEncodeRequest(t, destination))

			if err == nil || !strings.Contains(err.Error(), testCase.expected) || isFile(destination+partialSuffix) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFDKAACEncoder_Encode_SessionCannotOpen(t *testing.T) {
	requireFDKLibrary(t)
	encoder := NewFDKAACEncoder(sineDecoder{}, fdkLibraryPath())
	if err := encoder.Available(); err != nil {
		t.Fatal(err)
	}
	encoder.state.library.open = failingProcedure

	err := encoder.Encode(context.Background(), stereoEncodeRequest(t, filepath.Join(t.TempDir(), "out.m4a")))

	var fdkError FDKError
	if !errors.As(err, &fdkError) || fdkError.Operation != "aacEncOpen" {
		t.Fatalf("err = %v", err)
	}
}

func newTestPipeline(t *testing.T) *fdkPipeline {
	t.Helper()
	session := openTestSession(t)
	writer, err := NewMP4AudioWriter(filepath.Join(t.TempDir(), "out.m4a"), MP4AudioTrack{AudioSpecificConfig: session.AudioSpecificConfig(), Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Abort() })
	return &fdkPipeline{session: session, writer: writer, channels: 2}
}

func TestFDKPipeline_Failures(t *testing.T) {
	encodeFails := newTestPipeline(t)
	encodeFails.session.library.encode = failingProcedure
	cancelled := newTestPipeline(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flushFails := newTestPipeline(t)
	flushFails.session.library.encode = failingProcedure
	writeFails := newTestPipeline(t)
	if _, err := writeFails.session.Encode(sinePCM(0.5, 2)); err != nil {
		t.Fatal(err)
	}
	if err := writeFails.writer.Abort(); err != nil {
		t.Fatal(err)
	}

	encodeErr := encodeFails.run(context.Background(), stubbornDecoder{}, DecodeRequest{})
	cancelErr := cancelled.run(ctx, sineDecoder{}, DecodeRequest{})
	flushErr := flushFails.finish()
	writeErr := writeFails.finish()

	var fdkError FDKError
	if !errors.As(encodeErr, &fdkError) || !errors.Is(cancelErr, context.Canceled) || !errors.As(flushErr, &fdkError) || writeErr == nil || !strings.Contains(writeErr.Error(), "writing audio frame") {
		t.Fatalf("errors = %v %v %v %v", encodeErr, cancelErr, flushErr, writeErr)
	}
}

func TestMP4AudioWriter_Finish_MediaDataInUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.m4a")
	writer, err := NewMP4AudioWriter(path, MP4AudioTrack{Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(path + mp4MdatSuffix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	err = writer.Finish(1)

	if err == nil || isFile(path) {
		t.Fatalf("err = %v", err)
	}
}
