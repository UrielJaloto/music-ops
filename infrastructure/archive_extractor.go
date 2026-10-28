package infrastructure

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

var ErrArchiveEntryUnsafe = errors.New("archive entry escapes the destination folder")

type ZipExtractor struct{}

func NewZipExtractor() ZipExtractor {
	return ZipExtractor{}
}

func (extractor ZipExtractor) Extract(ctx context.Context, archive, destination string) error {
	reader, err := zip.OpenReader(filepath.FromSlash(archive))
	if err != nil {
		return fmt.Errorf("opening archive %s: %w", archive, err)
	}
	defer func() { _ = reader.Close() }()
	root := filepath.FromSlash(destination)
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := extractEntry(entry, root); err != nil {
			return err
		}
	}
	return nil
}

func extractEntry(entry *zip.File, root string) error {
	if !filepath.IsLocal(filepath.FromSlash(entry.Name)) {
		return fmt.Errorf("%w: %s", ErrArchiveEntryUnsafe, entry.Name)
	}
	target := filepath.Join(root, filepath.FromSlash(entry.Name))
	if entry.FileInfo().IsDir() {
		return os.MkdirAll(target, stagingFolderPermission)
	}
	if err := os.MkdirAll(filepath.Dir(target), stagingFolderPermission); err != nil {
		return fmt.Errorf("creating folder for %s: %w", entry.Name, err)
	}
	return writeEntry(entry, target)
}

func writeEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("reading %s: %w", entry.Name, err)
	}
	defer func() { _ = source.Close() }()
	output, err := os.OpenFile(filepath.Clean(target), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stagingFilePermission)
	if err != nil {
		return fmt.Errorf("creating %s: %w", target, err)
	}
	_, copyErr := io.Copy(output, io.LimitReader(source, clampInt64(entry.UncompressedSize64)))
	return errors.Join(copyErr, output.Close())
}

func clampInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
