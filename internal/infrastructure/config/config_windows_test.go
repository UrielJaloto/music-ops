//go:build windows

package config

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigStore_Save_FolderUnderAFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "blocker"), "x")
	store := NewConfigStore(filepath.Join(root, "blocker", "config.yaml"), environmentOf(nil))

	err := store.Save(context.Background(), "output.dir", "x")

	if err == nil || !strings.Contains(err.Error(), "creating configuration folder") {
		t.Fatalf("err = %v", err)
	}
}
