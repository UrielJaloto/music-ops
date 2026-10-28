package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	partialDownloadSuffix = ".part"
	downloadChunkBytes    = 1 << 20
)

var ErrArtifactHashMismatch = errors.New("downloaded artifact hash does not match")

type ArtifactProgress interface {
	ArtifactProgressed(name string, doneBytes int64)
}

type HTTPArtifactDownloader struct {
	http     *http.Client
	progress ArtifactProgress
}

func NewHTTPArtifactDownloader(http *http.Client, progress ArtifactProgress) HTTPArtifactDownloader {
	return HTTPArtifactDownloader{http: http, progress: progress}
}

func (downloader HTTPArtifactDownloader) Download(ctx context.Context, artifact services.ArtifactRequest, destination string) error {
	target := filepath.FromSlash(destination)
	if isFile(target) && fileHashMatches(target, artifact.SHA256) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), stagingFolderPermission); err != nil {
		return fmt.Errorf("creating download folder: %w", err)
	}
	partial := target + partialDownloadSuffix
	if err := downloader.fetch(ctx, artifact, partial); err != nil {
		return err
	}
	if !fileHashMatches(partial, artifact.SHA256) {
		return errors.Join(fmt.Errorf("%w: %s", ErrArtifactHashMismatch, artifact.Name), removeIfPresent(partial))
	}
	if err := os.Rename(partial, target); err != nil {
		return fmt.Errorf("renaming downloaded %s: %w", artifact.Name, err)
	}
	return nil
}

func (downloader HTTPArtifactDownloader) fetch(ctx context.Context, artifact services.ArtifactRequest, partial string) error {
	offset := existingSize(partial)
	response, err := downloader.request(ctx, artifact.URL, offset)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return removeIfPresent(partial)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("downloading %s: status %d", artifact.Name, response.StatusCode)
	}
	if response.StatusCode == http.StatusOK {
		offset = 0
	}
	return downloader.store(response.Body, partial, offset, artifact.Name)
}

func (downloader HTTPArtifactDownloader) request(ctx context.Context, address string, offset int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("building download request: %w", err)
	}
	if offset > 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	response, err := downloader.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", address, err)
	}
	return response, nil
}

func (downloader HTTPArtifactDownloader) store(body io.Reader, partial string, offset int64, name string) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if offset > 0 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := os.OpenFile(filepath.Clean(partial), flags, stagingFilePermission)
	if err != nil {
		return fmt.Errorf("opening %s: %w", partial, err)
	}
	copyErr := downloader.copyWithProgress(file, body, offset, name)
	return errors.Join(copyErr, file.Close())
}

func (downloader HTTPArtifactDownloader) copyWithProgress(file io.Writer, body io.Reader, offset int64, name string) error {
	buffer := make([]byte, downloadChunkBytes)
	done := offset
	for {
		read, readErr := body.Read(buffer)
		if _, err := file.Write(buffer[:read]); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		done += int64(read)
		downloader.progress.ArtifactProgressed(name, done)
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("downloading %s: %w", name, readErr)
		}
	}
}

func existingSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func fileHashMatches(path, expected string) bool {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), expected)
}
