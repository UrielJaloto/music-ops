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
	store := storeAt(filepath.Join(root, "blocker", "config.yaml"))

	err := store.Save(context.Background(), "output.dir", "x")

	if err == nil || !strings.Contains(err.Error(), "creating configuration folder") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindUserLocations_WindowsFolders(t *testing.T) {
	t.Setenv("APPDATA", `C:\Roaming`)
	t.Setenv("LOCALAPPDATA", `C:\Local`)
	t.Setenv("USERPROFILE", `C:\Users\someone`)

	locations := FindUserLocations()

	expected := Locations{ConfigDir: `C:\Roaming\music-ops`, WorkDir: `C:\Local\music-ops`, MusicDir: `C:\Users\someone\Music`}
	if locations != expected {
		t.Fatalf("locations = %+v", locations)
	}
}
