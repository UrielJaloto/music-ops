//go:build integration

package filesystem

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

func TestStagingArea_Unprepared(t *testing.T) {
	area := NewStagingArea(t.TempDir())
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 250)+".flac")

	_, shortErr := area.ShortenSourcePath(context.Background(), long)
	removeErr := area.RemoveOtherRuns()
	cleanupErr := area.Cleanup(context.Background())

	if !errors.Is(shortErr, importing.ErrWorkspaceClosed) || !errors.Is(removeErr, importing.ErrWorkspaceClosed) || cleanupErr != nil {
		t.Fatalf("errors = %v %v %v", shortErr, removeErr, cleanupErr)
	}
}

func TestStagingArea_Cleanup_ForgetsTheRun(t *testing.T) {
	area, _ := prepareTestStaging(t)

	cleanupErr := area.Cleanup(context.Background())
	removeErr := area.RemoveOtherRuns()

	if cleanupErr != nil || !errors.Is(removeErr, importing.ErrWorkspaceClosed) {
		t.Fatalf("errors = %v %v", cleanupErr, removeErr)
	}
}
