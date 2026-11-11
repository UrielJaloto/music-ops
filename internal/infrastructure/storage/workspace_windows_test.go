//go:build integration && windows

package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspace_Open_StaleRunCannotBeRemoved(t *testing.T) {
	fixture := newWorkspaceFixture(t.TempDir())
	stale := filepath.Join(fixture.workDir, "staging", "20260101-000000-dead", "track.m4a")
	writeWorkspaceFile(t, stale)
	locked, err := os.Open(stale)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locked.Close() })

	openErr := fixture.workspace().Open(context.Background())

	if openErr == nil {
		t.Fatal("open succeeded with a locked stale run")
	}
	if _, writeErr := fixture.runLog.Write([]byte("x")); writeErr == nil {
		t.Fatal("run log left open")
	}
}
