package artifacts

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/UrielJaloto/music-ops/internal/services/setup"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

var errWrite = errors.New("write")

func TestHTTPArtifactDownloader_FileSystemFailures(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(t *testing.T, root string) string
		expected string
	}{
		{name: "FolderUnderAFile", prepare: func(t *testing.T, root string) string {
			writeTestFile(t, filepath.Join(root, "blocker"), "x")
			return filepath.Join(root, "blocker", "model.gguf")
		}, expected: "creating download folder"},
		{name: "PartialIsAFolder", prepare: func(t *testing.T, root string) string {
			target := filepath.Join(root, "model.gguf")
			writeTestFile(t, filepath.Join(target+partialDownloadSuffix, "inside"), "x")
			return target
		}, expected: "opening"},
		{name: "TargetIsAFolder", prepare: func(t *testing.T, root string) string {
			target := filepath.Join(root, "model.gguf")
			writeTestFile(t, filepath.Join(target, "inside"), "x")
			return target
		}, expected: "renaming downloaded model.gguf"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			artifact, client := serveArtifact(t, &artifactServer{content: []byte("model")})
			destination := testCase.prepare(t, t.TempDir())

			err := NewHTTPArtifactDownloader(client, &progressSpy{}).Download(context.Background(), artifact, filepath.ToSlash(destination))

			if err == nil || !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestHTTPArtifactDownloader_InvalidURL(t *testing.T) {
	artifact := setup.ArtifactRequest{Name: "x", URL: "http://\x7f/x"}

	err := NewHTTPArtifactDownloader(http.DefaultClient, &progressSpy{}).Download(context.Background(), artifact, filepath.Join(t.TempDir(), "x"))

	if err == nil || !strings.Contains(err.Error(), "building download request") {
		t.Fatalf("err = %v", err)
	}
}

func TestCopyWithProgress_Failures(t *testing.T) {
	errRead := errors.New("read")
	downloader := NewHTTPArtifactDownloader(http.DefaultClient, &progressSpy{})

	writeErr := downloader.copyWithProgress(failingWriter{}, strings.NewReader("x"), 0, "model")
	readErr := downloader.copyWithProgress(io.Discard, iotest.ErrReader(errRead), 0, "model")

	if !errors.Is(writeErr, errWrite) || !errors.Is(readErr, errRead) || readErr.Error() != "downloading model: read" {
		t.Fatalf("errors = %v %v", writeErr, readErr)
	}
}

func TestHasMatchingHash_Folder_IsFalse(t *testing.T) {
	if hasMatchingHash(t.TempDir(), hashOf(nil)) {
		t.Fatal("folder matched")
	}
}

func TestRemoveIfPresent_FolderWithContent_Fails(t *testing.T) {
	folder := t.TempDir()
	writeTestFile(t, filepath.Join(folder, "inside"), "x")

	if err := removeIfPresent(folder); err == nil {
		t.Fatal("folder removed")
	}
}

func writeRawZip(t *testing.T, header *zip.FileHeader, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.CreateRaw(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte(content))
	if err := errors.Join(writer.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestZipExtractor_EntryFailures(t *testing.T) {
	cases := []struct {
		name     string
		archive  func(t *testing.T) string
		prepare  func(t *testing.T, root string) string
		expected string
	}{
		{name: "FolderUnderAFile", archive: func(t *testing.T) string {
			return writeZip(t, map[string]string{"lib/a.dll": "a"})
		}, prepare: func(t *testing.T, root string) string {
			writeTestFile(t, filepath.Join(root, "blocker"), "x")
			return filepath.Join(root, "blocker")
		}, expected: "creating folder for lib/a.dll"},
		{name: "UnsupportedCompression", archive: func(t *testing.T) string {
			return writeRawZip(t, &zip.FileHeader{Name: "a.dll", Method: 99, CompressedSize64: 1, UncompressedSize64: 1}, "a")
		}, prepare: func(_ *testing.T, root string) string { return root }, expected: "reading a.dll"},
		{name: "TargetIsAFolder", archive: func(t *testing.T) string {
			return writeZip(t, map[string]string{"a.dll": "a"})
		}, prepare: func(t *testing.T, root string) string {
			writeTestFile(t, filepath.Join(root, "a.dll", "inside"), "x")
			return root
		}, expected: "creating"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			archive := testCase.archive(t)
			destination := testCase.prepare(t, t.TempDir())

			err := NewZipExtractor().Extract(context.Background(), filepath.ToSlash(archive), filepath.ToSlash(destination))

			if err == nil || !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
