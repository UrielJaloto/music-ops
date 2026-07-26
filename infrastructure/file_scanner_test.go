//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/services"
)

func createFiles(t *testing.T, root string, relativePaths ...string) {
	t.Helper()
	for _, relative := range relativePaths {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)), "data")
	}
}

func relativePathsOf(files []services.FoundFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
	}
	return paths
}

func TestFileScanner_FindAudioFiles_MixedTree(t *testing.T) {
	root := t.TempDir()
	createFiles(t, root,
		"b/song.FLAC", "a/track.mp3", "a/cover.jpg", "notes.txt", "c/deep/x.Opus",
		".hidden/secret.flac", ".music-ops/staged.m4a", "$RECYCLE.BIN/old.mp3", "System Volume Information/v.wav",
	)

	found, err := NewFileScanner().FindAudioFiles(context.Background(), root)

	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"a/track.mp3", "b/song.FLAC", "c/deep/x.Opus"}, relativePathsOf(found.Audio)); diff != "" {
		t.Fatalf("audio (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"a/cover.jpg", "notes.txt"}, found.Ignored); diff != "" {
		t.Fatalf("ignored (-want +got):\n%s", diff)
	}
	first := found.Audio[0]
	if first.Path != filepath.Join(root, "a", "track.mp3") || first.Directory != filepath.Join(root, "a") || first.Size != 4 || first.ModifiedAt.IsZero() {
		t.Fatalf("file = %+v", first)
	}
}

func TestFileScanner_FindAudioFiles_EveryAudioExtension(t *testing.T) {
	root := t.TempDir()
	expected := make([]string, 0, len(audioExtensions))
	for _, extension := range audioExtensions {
		name := "song" + strings.ToUpper(extension)
		createFiles(t, root, name)
		expected = append(expected, name)
	}

	found, err := NewFileScanner().FindAudioFiles(context.Background(), root)

	if err != nil || len(found.Audio) != len(expected) || len(found.Ignored) != 0 {
		t.Fatalf("audio = %v ignored = %v err = %v", relativePathsOf(found.Audio), found.Ignored, err)
	}
}

func TestFileScanner_FindAudioFiles_NFDNameReturnedAsNFC(t *testing.T) {
	root := t.TempDir()
	decomposed := "Benção.flac"
	createFiles(t, root, decomposed)

	found, err := NewFileScanner().FindAudioFiles(context.Background(), root)

	if err != nil || len(found.Audio) != 1 || found.Audio[0].RelativePath != "Benção.flac" {
		t.Fatalf("audio = %+v err = %v", found.Audio, err)
	}
	if _, statErr := os.Stat(found.Audio[0].Path); statErr != nil {
		t.Fatalf("path is not openable: %v", statErr)
	}
}

func TestFileScanner_FindAudioFiles_LongPath(t *testing.T) {
	root := t.TempDir()
	relative := strings.Repeat("a", 100) + "/" + strings.Repeat("b", 100) + "/" + strings.Repeat("c", 80) + ".flac"
	createFiles(t, root, relative)

	found, err := NewFileScanner().FindAudioFiles(context.Background(), root)

	if err != nil || len(found.Audio) != 1 || len(found.Audio[0].Path) <= 260 || found.Audio[0].RelativePath != relative {
		t.Fatalf("audio = %+v err = %v", found.Audio, err)
	}
}

func TestFileScanner_FindAudioFiles_Cancelled(t *testing.T) {
	root := t.TempDir()
	createFiles(t, root, "a.flac")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewFileScanner().FindAudioFiles(ctx, root)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFileScanner_FindAudioFiles_MissingFolder(t *testing.T) {
	cases := []struct {
		name   string
		folder func(t *testing.T) string
	}{
		{name: "DoesNotExist", folder: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		{name: "IsAFile", folder: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file.flac")
			writeTestFile(t, path, "x")
			return path
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewFileScanner().FindAudioFiles(context.Background(), testCase.folder(t))

			if !errors.Is(err, services.ErrFolderMissing) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLibraryIndex_ListTrackPaths(t *testing.T) {
	root := t.TempDir()
	createFiles(t, root, "Artist - Song.m4a", "Album/01 - Intro.M4A", "Album/cover.jpg", "other.mp3", ".music-ops/staging/x.m4a")

	paths, err := NewLibraryIndex().ListTrackPaths(context.Background(), root)

	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"Album/01 - Intro.M4A", "Artist - Song.m4a"}, paths); diff != "" {
		t.Fatalf("paths (-want +got):\n%s", diff)
	}
}

func TestLibraryIndex_ListTrackPaths_MissingRoot(t *testing.T) {
	paths, err := NewLibraryIndex().ListTrackPaths(context.Background(), filepath.Join(t.TempDir(), "missing"))

	if err != nil || paths == nil || len(paths) != 0 {
		t.Fatalf("paths = %v err = %v", paths, err)
	}
}

func TestLibraryIndex_ListTrackPaths_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewLibraryIndex().ListTrackPaths(ctx, t.TempDir())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
