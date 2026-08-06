//go:build e2e

package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

const e2eCacheFolder = "music-ops-test-cache"

type fixtureTags struct {
	title       string
	artist      string
	albumArtist string
	album       string
	date        string
	track       int
	trackTotal  int
}

type fixture struct {
	path       string
	format     string
	tags       fixtureTags
	sampleRate int
	channels   int
	bitDepth   int
	seconds    float64
	hasCover   bool
	isCorrupt  bool
}

var fixtureCodecs = map[string]func(fixture) []string{
	"flac": func(item fixture) []string {
		if item.bitDepth > 16 {
			return []string{"-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", strconv.Itoa(item.bitDepth)}
		}
		return []string{"-c:a", "flac", "-sample_fmt", "s16"}
	},
	"wav": func(fixture) []string { return []string{"-c:a", "pcm_s16le"} },
	"mp3": func(fixture) []string { return []string{"-c:a", "libmp3lame", "-b:a", "192k"} },
	"m4a": func(fixture) []string { return []string{"-c:a", "aac", "-b:a", "192k"} },
}

func writeFixtures(t *testing.T, ffmpeg, folder string, fixtures ...fixture) {
	t.Helper()
	for _, item := range fixtures {
		cached := cachedE2EFixture(t, ffmpeg, item)
		target := filepath.Join(folder, filepath.FromSlash(item.path))
		data, err := os.ReadFile(filepath.Clean(cached))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func cachedE2EFixture(t *testing.T, ffmpeg string, item fixture) string {
	t.Helper()
	spec := item
	spec.path = filepath.Ext(item.path)
	digest := sha1.Sum(fmt.Appendf(nil, "e2e%+v", spec))
	root, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, e2eCacheFolder, hex.EncodeToString(digest[:]))
	path := filepath.Join(folder, "fixture"+spec.path)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	generateE2EFixture(t, ffmpeg, item, path)
	return path
}

func generateE2EFixture(t *testing.T, ffmpeg string, item fixture, path string) {
	t.Helper()
	partial := path + ".partial" + filepath.Ext(path)
	arguments := []string{"-y", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=%d:duration=%g", item.sampleRate, item.seconds)}
	if item.hasCover {
		arguments = append(arguments, "-f", "lavfi", "-i", "color=c=blue:s=64x64:d=1", "-frames:v", "1", "-map", "0:a", "-map", "1:v", "-c:v", "png", "-disposition:v", "attached_pic")
	}
	arguments = append(arguments, "-ac", strconv.Itoa(item.channels))
	arguments = append(arguments, fixtureCodecs[item.format](item)...)
	arguments = append(arguments, fixtureMetadata(item.tags)...)
	arguments = append(arguments, partial)
	if output, err := exec.CommandContext(context.Background(), ffmpeg, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("generating %s: %v\n%s", item.path, err, output)
	}
	if item.isCorrupt {
		if err := os.Truncate(partial, 1024); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(partial, path); err != nil {
		t.Fatal(err)
	}
}

func fixtureMetadata(tags fixtureTags) []string {
	values := [][2]string{{"title", tags.title}, {"artist", tags.artist}, {"album_artist", tags.albumArtist}, {"album", tags.album}, {"date", tags.date}}
	if tags.track > 0 {
		track := strconv.Itoa(tags.track)
		if tags.trackTotal > 0 {
			track += "/" + strconv.Itoa(tags.trackTotal)
		}
		values = append(values, [2]string{"track", track})
	}
	arguments := []string{}
	for _, value := range values {
		if value[1] != "" {
			arguments = append(arguments, "-metadata", value[0]+"="+value[1])
		}
	}
	return arguments
}
