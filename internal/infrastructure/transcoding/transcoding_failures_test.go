package transcoding

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/infrastructure/process"
)

type buildConfigurationFailure struct{}

var errBuildConfiguration = errors.New("buildconf")

func (buildConfigurationFailure) Run(_ context.Context, request process.ProcessRequest) (process.ProcessResult, error) {
	if slices.Contains(request.Arguments, "-buildconf") {
		return process.ProcessResult{}, errBuildConfiguration
	}
	return process.ProcessResult{}, nil
}

func TestDetectFFmpegCapabilities_BuildConfigurationFails(t *testing.T) {
	_, err := DetectFFmpegCapabilities(context.Background(), buildConfigurationFailure{}, "ffmpeg")

	if !errors.Is(err, errBuildConfiguration) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveIfPresent_FolderWithContent_Fails(t *testing.T) {
	folder := t.TempDir()
	writeTestFile(t, filepath.Join(folder, "inside"), "x")

	if err := removeIfPresent(folder); err == nil {
		t.Fatal("folder removed")
	}
}

func TestMP4AudioWriter_Header_LargeMediaUsesLargeOffsets(t *testing.T) {
	writer, err := NewMP4AudioWriter(filepath.Join(t.TempDir(), "x.m4a"), MP4AudioTrack{Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Abort() })
	writer.mdatBytes = 1 << 32

	header := writer.buildHeader(1)

	if !bytes.Contains(header, []byte("co64")) || bytes.Contains(header, []byte("stco")) {
		t.Fatal("large offsets not used")
	}
}

func TestMP4AudioWriter_Finish_FileSystemFailures(t *testing.T) {
	cases := []struct {
		name     string
		blocked  string
		expected string
	}{
		{name: "ComposedFileIsAFolder", blocked: mp4ComposedSuffix, expected: "creating"},
		{name: "DestinationIsAFolder", blocked: "", expected: "renaming"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "x.m4a")
			writer, err := NewMP4AudioWriter(path, MP4AudioTrack{Channels: 2})
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(path+testCase.blocked, "inside"), "x")

			err = writer.Finish(1)

			if err == nil || !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

type failingWriter struct{}

var errWrite = errors.New("write")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestMP4AudioWriter_WriteMdat_Failures(t *testing.T) {
	writer, err := NewMP4AudioWriter(filepath.Join(t.TempDir(), "x.m4a"), MP4AudioTrack{Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	headerErr := writer.writeMdat(failingWriter{})
	if err := writer.Abort(); err != nil {
		t.Fatal(err)
	}

	copyErr := writer.writeMdat(io.Discard)

	if !errors.Is(headerErr, errWrite) || copyErr == nil || !strings.Contains(copyErr.Error(), "copying media data") {
		t.Fatalf("errors = %v %v", headerErr, copyErr)
	}
}
