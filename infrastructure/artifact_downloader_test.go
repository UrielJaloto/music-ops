package infrastructure

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/UrielJaloto/music-ops/services"
)

type progressSpy struct {
	mutex sync.Mutex
	done  []int64
}

func (spy *progressSpy) ArtifactProgressed(_ string, doneBytes int64) {
	spy.mutex.Lock()
	defer spy.mutex.Unlock()
	spy.done = append(spy.done, doneBytes)
}

func (spy *progressSpy) last() int64 {
	spy.mutex.Lock()
	defer spy.mutex.Unlock()
	return spy.done[len(spy.done)-1]
}

type artifactServer struct {
	content       []byte
	supportsRange bool
	requests      int
	ranges        []string
}

func (server *artifactServer) handler(writer http.ResponseWriter, request *http.Request) {
	server.requests++
	requested := request.Header.Get("Range")
	server.ranges = append(server.ranges, requested)
	if !server.supportsRange || requested == "" {
		_, _ = writer.Write(server.content)
		return
	}
	start, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(requested, "bytes="), "-"))
	if start >= len(server.content) {
		writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	writer.WriteHeader(http.StatusPartialContent)
	_, _ = writer.Write(server.content[start:])
}

func hashOf(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func serveArtifact(t *testing.T, server *artifactServer) (services.ArtifactRequest, *http.Client) {
	t.Helper()
	httpServer := httptest.NewServer(http.HandlerFunc(server.handler))
	t.Cleanup(httpServer.Close)
	return services.ArtifactRequest{Name: "model.gguf", URL: httpServer.URL + "/model.gguf", SHA256: hashOf(server.content), SizeBytes: int64(len(server.content))}, httpServer.Client()
}

func TestHTTPArtifactDownloader_FreshDownload(t *testing.T) {
	content := bytes.Repeat([]byte("weights"), 1000)
	server := &artifactServer{content: content, supportsRange: true}
	artifact, client := serveArtifact(t, server)
	destination := filepath.ToSlash(filepath.Join(t.TempDir(), "models", "model.gguf"))
	progress := &progressSpy{}

	err := NewHTTPArtifactDownloader(client, progress).Download(context.Background(), artifact, destination)

	stored, readErr := os.ReadFile(filepath.Clean(filepath.FromSlash(destination)))
	if err != nil || readErr != nil || !bytes.Equal(stored, content) || progress.last() != int64(len(content)) || isFile(filepath.FromSlash(destination)+".part") {
		t.Fatalf("err = %v progress = %v", err, progress.done)
	}
}

func TestHTTPArtifactDownloader_ResumesPartialDownload(t *testing.T) {
	cases := []struct {
		name          string
		supportsRange bool
	}{
		{name: "RangeSupported_Appends", supportsRange: true},
		{name: "RangeIgnored_Restarts", supportsRange: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			content := bytes.Repeat([]byte("0123456789"), 700)
			server := &artifactServer{content: content, supportsRange: testCase.supportsRange}
			artifact, client := serveArtifact(t, server)
			destination := filepath.Join(t.TempDir(), "model.gguf")
			writeTestFile(t, destination+".part", string(content[:3000]))

			err := NewHTTPArtifactDownloader(client, &progressSpy{}).Download(context.Background(), artifact, destination)

			stored, _ := os.ReadFile(filepath.Clean(destination))
			if err != nil || !bytes.Equal(stored, content) || server.ranges[0] != "bytes=3000-" {
				t.Fatalf("err = %v ranges = %v size = %d", err, server.ranges, len(stored))
			}
		})
	}
}

func TestHTTPArtifactDownloader_ExistingFileWithMatchingHash(t *testing.T) {
	content := []byte("already here")
	server := &artifactServer{content: content}
	artifact, client := serveArtifact(t, server)
	destination := filepath.Join(t.TempDir(), "model.gguf")
	writeTestFile(t, destination, string(content))

	err := NewHTTPArtifactDownloader(client, &progressSpy{}).Download(context.Background(), artifact, destination)

	if err != nil || server.requests != 0 {
		t.Fatalf("err = %v requests = %d", err, server.requests)
	}
}

func TestHTTPArtifactDownloader_HashMismatch(t *testing.T) {
	server := &artifactServer{content: []byte("tampered")}
	artifact, client := serveArtifact(t, server)
	artifact.SHA256 = hashOf([]byte("expected"))
	destination := filepath.Join(t.TempDir(), "model.gguf")

	err := NewHTTPArtifactDownloader(client, &progressSpy{}).Download(context.Background(), artifact, destination)

	if !errors.Is(err, ErrArtifactHashMismatch) || isFile(destination) || isFile(destination+".part") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPArtifactDownloader_OversizedPartialIsDiscarded(t *testing.T) {
	content := []byte("complete content")
	server := &artifactServer{content: content, supportsRange: true}
	artifact, client := serveArtifact(t, server)
	destination := filepath.Join(t.TempDir(), "model.gguf")
	writeTestFile(t, destination+".part", string(content)+"junk")

	err := NewHTTPArtifactDownloader(client, &progressSpy{}).Download(context.Background(), artifact, destination)

	if !errors.Is(err, ErrArtifactHashMismatch) || isFile(destination+".part") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPArtifactDownloader_ServerErrors(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNotFound) }))
	defer failing.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	downloader := NewHTTPArtifactDownloader(http.DefaultClient, &progressSpy{})

	notFoundErr := downloader.Download(context.Background(), services.ArtifactRequest{Name: "x", URL: failing.URL}, filepath.Join(t.TempDir(), "x"))
	downErr := downloader.Download(context.Background(), services.ArtifactRequest{Name: "x", URL: down.URL}, filepath.Join(t.TempDir(), "x"))

	if notFoundErr == nil || !strings.Contains(notFoundErr.Error(), "status 404") || downErr == nil {
		t.Fatalf("errors = %v %v", notFoundErr, downErr)
	}
}

func writeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestZipExtractor_NestedFolders(t *testing.T) {
	archive := writeZip(t, map[string]string{"llama-server.exe": "exe", "lib/cuda/cudart.dll": "dll", "docs/": ""})
	destination := filepath.Join(t.TempDir(), "llama.cpp")

	err := NewZipExtractor().Extract(context.Background(), filepath.ToSlash(archive), filepath.ToSlash(destination))

	server, _ := os.ReadFile(filepath.Clean(filepath.Join(destination, "llama-server.exe")))
	library, _ := os.ReadFile(filepath.Clean(filepath.Join(destination, "lib", "cuda", "cudart.dll")))
	if err != nil || string(server) != "exe" || string(library) != "dll" || !pathExists(filepath.Join(destination, "docs")) {
		t.Fatalf("err = %v", err)
	}
}

func TestZipExtractor_RejectsZipSlip(t *testing.T) {
	cases := []string{"../evil.exe", "a/../../evil.exe", "/abs/evil.exe"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			archive := writeZip(t, map[string]string{name: "evil"})
			destination := filepath.Join(t.TempDir(), "out")

			err := NewZipExtractor().Extract(context.Background(), archive, destination)

			if !errors.Is(err, ErrArchiveEntryUnsafe) || pathExists(filepath.Join(filepath.Dir(destination), "evil.exe")) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestZipExtractor_Errors(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "bad.zip")
	writeTestFile(t, notZip, "not a zip")
	archive := writeZip(t, map[string]string{"a.txt": "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	openErr := NewZipExtractor().Extract(context.Background(), notZip, t.TempDir())
	cancelErr := NewZipExtractor().Extract(ctx, archive, t.TempDir())

	if openErr == nil || !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("errors = %v %v", openErr, cancelErr)
	}
}

func TestAdvisorArtifacts_ArePinned(t *testing.T) {
	artifacts := append(AdvisorRuntimeArtifacts(), AdvisorModelArtifact())

	for _, artifact := range artifacts {
		if len(artifact.SHA256) != 64 || artifact.SizeBytes <= 0 || !strings.HasPrefix(artifact.URL, "https://") || !strings.HasSuffix(artifact.URL, artifact.Name) {
			t.Fatalf("artifact = %+v", artifact)
		}
	}
	if !strings.Contains(AdvisorModelArtifact().URL, "/resolve/3885219b") || !strings.Contains(artifacts[0].URL, "/download/b11606/") {
		t.Fatal("artifacts are not pinned to a revision")
	}
}
