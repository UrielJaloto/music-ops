//go:build integration

package infrastructure

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	fixtureCacheFolder   = "music-ops-test-cache"
	fixtureCorruptSize   = 1024
	fixtureSineFrequency = 440
	fixtureCoverSize     = "64x64"
	fixtureOpusRate      = 48000
)

type audioFixtureTags struct {
	title       string
	artist      string
	albumArtist string
	album       string
	date        string
	track       int
	trackTotal  int
	disc        int
}

type audioFixtureSignal struct {
	sampleRate int
	channels   int
	bitDepth   int
	seconds    float64
}

type audioFixture struct {
	relativePath string
	format       string
	tags         audioFixtureTags
	signal       audioFixtureSignal
	hasCover     bool
	isCorrupt    bool
}

var fixtureCodecArguments = map[string]func(signal audioFixtureSignal) []string{
	"flac": func(signal audioFixtureSignal) []string {
		return append([]string{"-c:a", "flac"}, flacSampleFormat(signal.bitDepth)...)
	},
	"wav": func(signal audioFixtureSignal) []string {
		return []string{"-c:a", "pcm_s" + strconv.Itoa(max(signal.bitDepth, 16)) + "le"}
	},
	"mp3":  func(audioFixtureSignal) []string { return []string{"-c:a", "libmp3lame", "-b:a", "192k"} },
	"m4a":  func(audioFixtureSignal) []string { return []string{"-c:a", "aac", "-b:a", "192k"} },
	"opus": func(audioFixtureSignal) []string { return []string{"-c:a", "libopus", "-b:a", "128k"} },
}

func flacSampleFormat(bitDepth int) []string {
	if bitDepth > 16 {
		return []string{"-sample_fmt", "s32", "-bits_per_raw_sample", strconv.Itoa(bitDepth)}
	}
	return []string{"-sample_fmt", "s16"}
}

func requireFFmpeg(t *testing.T) (string, string) {
	t.Helper()
	localAppData, _ := os.LookupEnv("LOCALAPPDATA")
	ffmpeg, ffmpegErr := LocateFFmpeg("", exec.LookPath, localAppData)
	ffprobe, ffprobeErr := LocateFFprobe("", exec.LookPath, localAppData)
	if ffmpegErr == nil && ffprobeErr == nil {
		return ffmpeg, ffprobe
	}
	if strict, _ := os.LookupEnv("MUSIC_OPS_STRICT"); strict == "1" {
		t.Fatalf("ffmpeg is required: %v %v", ffmpegErr, ffprobeErr)
	}
	t.Skipf("ffmpeg is not installed: %v %v", ffmpegErr, ffprobeErr)
	return "", ""
}

func buildAudioFixtures(t *testing.T, folder string, fixtures []audioFixture) []string {
	t.Helper()
	ffmpeg, _ := requireFFmpeg(t)
	paths := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		cached := cachedFixture(t, ffmpeg, fixture)
		target := filepath.Join(folder, filepath.FromSlash(fixture.relativePath))
		copyFixture(t, cached, target)
		paths = append(paths, target)
	}
	return paths
}

func cachedFixture(t *testing.T, ffmpeg string, fixture audioFixture) string {
	t.Helper()
	spec := fixture
	spec.relativePath = filepath.Ext(fixture.relativePath)
	digest := sha1.Sum(fmt.Appendf(nil, "%+v", spec))
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(cacheRoot, fixtureCacheFolder, hex.EncodeToString(digest[:]))
	path := filepath.Join(folder, "fixture"+spec.relativePath)
	if isFile(path) {
		return path
	}
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	generateFixture(t, ffmpeg, fixture, path)
	return path
}

func generateFixture(t *testing.T, ffmpeg string, fixture audioFixture, path string) {
	t.Helper()
	temporary := path + ".partial" + filepath.Ext(path)
	arguments := fixtureArguments(t, ffmpeg, fixture, temporary)
	if output, err := exec.CommandContext(context.Background(), ffmpeg, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("generating fixture %s: %v\n%s", fixture.relativePath, err, output)
	}
	if fixture.isCorrupt {
		truncateFixture(t, temporary)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func fixtureArguments(t *testing.T, ffmpeg string, fixture audioFixture, output string) []string {
	t.Helper()
	signal := fixture.signal
	sampleRate := signal.sampleRate
	if fixture.format == "opus" {
		sampleRate = fixtureOpusRate
	}
	source := fmt.Sprintf("sine=frequency=%d:sample_rate=%d:duration=%g", fixtureSineFrequency, sampleRate, signal.seconds)
	arguments := []string{"-y", "-v", "error", "-f", "lavfi", "-i", source}
	if fixture.hasCover {
		arguments = append(arguments, "-i", fixtureCover(t, ffmpeg))
		arguments = append(arguments, "-map", "0:a", "-map", "1:v", "-c:v", "png", "-disposition:v", "attached_pic")
	}
	codec, isKnown := fixtureCodecArguments[fixture.format]
	if !isKnown {
		t.Fatalf("unknown fixture format %s", fixture.format)
	}
	arguments = append(arguments, "-ac", strconv.Itoa(max(signal.channels, 1)))
	arguments = append(arguments, codec(signal)...)
	arguments = append(arguments, fixtureMetadata(fixture.tags)...)
	return append(arguments, output)
}

func fixtureMetadata(tags audioFixtureTags) []string {
	values := map[string]string{
		"title": tags.title, "artist": tags.artist, "album_artist": tags.albumArtist,
		"album": tags.album, "date": tags.date,
	}
	if tags.track > 0 {
		values["track"] = trackValue(tags.track, tags.trackTotal)
	}
	if tags.disc > 0 {
		values["disc"] = strconv.Itoa(tags.disc)
	}
	arguments := []string{}
	for key, value := range values {
		if value != "" {
			arguments = append(arguments, "-metadata", key+"="+value)
		}
	}
	return arguments
}

func trackValue(track, total int) string {
	if total > 0 {
		return strconv.Itoa(track) + "/" + strconv.Itoa(total)
	}
	return strconv.Itoa(track)
}

func fixtureCover(t *testing.T, ffmpeg string) string {
	t.Helper()
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cacheRoot, fixtureCacheFolder, "cover.png")
	if isFile(path) {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	temporary := path + ".partial.png"
	arguments := []string{"-y", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=" + fixtureCoverSize, "-frames:v", "1", temporary}
	if output, err := exec.CommandContext(context.Background(), ffmpeg, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("generating cover: %v\n%s", err, output)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func truncateFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.Truncate(path, fixtureCorruptSize); err != nil {
		t.Fatal(err)
	}
}

func copyFixture(t *testing.T, source, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var content bytes.Buffer
	if _, err := io.Copy(&content, input); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

type probedStream struct {
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Profile       string            `json:"profile"`
	BitRate       string            `json:"bit_rate"`
	SampleRate    string            `json:"sample_rate"`
	Channels      int               `json:"channels"`
	BitsPerSample string            `json:"bits_per_raw_sample"`
	Tags          map[string]string `json:"tags"`
}

type probedFile struct {
	Streams []probedStream `json:"streams"`
	Format  struct {
		Tags      map[string]string `json:"tags"`
		Duration  string            `json:"duration"`
		StartTime string            `json:"start_time"`
		BitRate   string            `json:"bit_rate"`
	} `json:"format"`
}

func (probed probedFile) allTags() map[string]string {
	tags := map[string]string{}
	for _, stream := range probed.Streams {
		addLowerCaseTags(tags, stream.Tags)
	}
	addLowerCaseTags(tags, probed.Format.Tags)
	return tags
}

func addLowerCaseTags(target, source map[string]string) {
	for key, value := range source {
		target[strings.ToLower(key)] = value
	}
}

func probeFixture(t *testing.T, path string) probedFile {
	t.Helper()
	_, ffprobe := requireFFmpeg(t)
	var stdout bytes.Buffer
	request := ProcessRequest{Executable: ffprobe, Arguments: []string{"-v", "error", "-show_streams", "-show_format", "-of", "json", path}, Stdout: &stdout}
	if _, err := NewProcessRunner().Run(context.Background(), request); err != nil {
		t.Fatalf("probing %s: %v", path, err)
	}
	var probed probedFile
	if err := json.Unmarshal(stdout.Bytes(), &probed); err != nil {
		t.Fatalf("decoding probe of %s: %v", path, err)
	}
	return probed
}
