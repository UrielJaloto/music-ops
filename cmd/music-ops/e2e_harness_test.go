//go:build e2e

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const defaultFDKLibrary = `C:\Program Files\freac\codecs\FDK-AAC.dll`

var e2eBinary string

func TestMain(m *testing.M) {
	os.Exit(runE2E(m))
}

func runE2E(m *testing.M) int {
	folder, err := os.MkdirTemp("", "music-ops-e2e-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(folder) }()
	e2eBinary = filepath.Join(folder, "music-ops.exe")
	build := exec.CommandContext(context.Background(), "go", "build", "-tags", "e2e", "-o", e2eBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(output), err)
		return 1
	}
	return m.Run()
}

type e2eEnvironment struct {
	t            *testing.T
	root         string
	input        string
	output       string
	appData      string
	localAppData string
	ffmpeg       string
	ffprobe      string
	lastfm       *fakeLastFM
	localOracle  *adviceOracle
	llama        *fakeLlama
	settings     map[string]string
	extraEnv     []string
}

type e2eResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Report   reportJSON
}

type reportJSON struct {
	RunID      string            `json:"run_id"`
	ExitStatus int               `json:"exit_status"`
	Counts     map[string]int    `json:"counts"`
	Warnings   []string          `json:"warnings"`
	Tracks     []reportTrackJSON `json:"tracks"`
}

type reportTrackJSON struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Genre  string `json:"genre"`
	Status string `json:"status"`
	Reason string `json:"error"`
}

func newE2EEnvironment(t *testing.T) *e2eEnvironment {
	t.Helper()
	ffmpeg, ffprobe := locateTools(t)
	root := t.TempDir()
	environment := &e2eEnvironment{
		t: t, root: root, input: filepath.Join(root, "in"), output: filepath.Join(root, "out"),
		appData: filepath.Join(root, "appdata"), localAppData: filepath.Join(root, "localappdata"),
		ffmpeg: ffmpeg, ffprobe: ffprobe, lastfm: newFakeLastFM(t),
		localOracle: newAdviceOracle(),
	}
	environment.llama = newFakeLlama(t, environment.localOracle)
	environment.settings = environment.defaultSettings()
	return environment
}

func locateTools(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
	ffprobe, ffprobeErr := exec.LookPath("ffprobe")
	if ffmpegErr != nil || ffprobeErr != nil {
		t.Fatalf("ffmpeg and ffprobe are required on PATH: %v %v", ffmpegErr, ffprobeErr)
	}
	return ffmpeg, ffprobe
}

func (environment *e2eEnvironment) defaultSettings() map[string]string {
	llamaURL, _ := url.Parse(environment.llama.server.URL)
	return map[string]string{
		"output.dir":          environment.output,
		"encoder.ffmpeg":      environment.ffmpeg,
		"encoder.ffprobe":     environment.ffprobe,
		"encoder.fdk_library": fdkLibrary(),
		"lastfm.api_key":      "test-key",
		"lastfm.base_url":     environment.lastfm.server.URL + "/2.0/",
		"advisor.port":        llamaURL.Port(),
		"advisor.model_file":  "e2e-model.gguf",
	}
}

func fdkLibrary() string {
	if path, isSet := os.LookupEnv("MUSIC_OPS_ENCODER_FDK_LIBRARY"); isSet && path != "" {
		return path
	}
	return defaultFDKLibrary
}

func (environment *e2eEnvironment) useNativeEncoder() {
	environment.settings["encoder.fdk_library"] = filepath.Join(environment.root, "missing", "FDK-AAC.dll")
}

func (environment *e2eEnvironment) writeConfig() {
	environment.t.Helper()
	sections := map[string]map[string]string{}
	for key, value := range environment.settings {
		section, name, _ := strings.Cut(key, ".")
		if sections[section] == nil {
			sections[section] = map[string]string{}
		}
		sections[section][name] = value
	}
	var config strings.Builder
	for _, section := range sortedKeys(sections) {
		config.WriteString(section + ":\n")
		for _, name := range sortedKeys(sections[section]) {
			config.WriteString("  " + name + ": " + strconv.Quote(sections[section][name]) + "\n")
		}
	}
	path := filepath.Join(environment.appData, "music-ops", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		environment.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(config.String()), 0o600); err != nil {
		environment.t.Fatal(err)
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (environment *e2eEnvironment) childEnvironment() []string {
	isolated := []string{"APPDATA=" + environment.appData, "LOCALAPPDATA=" + environment.localAppData}
	inherited := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains([]string{"APPDATA", "LOCALAPPDATA"}, strings.ToUpper(name)) || strings.HasPrefix(strings.ToUpper(name), "MUSIC_OPS_") {
			continue
		}
		inherited = append(inherited, entry)
	}
	return append(append(inherited, isolated...), environment.extraEnv...)
}

func (environment *e2eEnvironment) run(arguments ...string) e2eResult {
	environment.t.Helper()
	environment.writeConfig()
	previous := environment.reportPaths()
	command := exec.CommandContext(context.Background(), e2eBinary, append(arguments, "--no-open")...)
	command.Env = environment.childEnvironment()
	command.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	result := e2eResult{ExitCode: exitCodeOf(err), Stdout: stdout.String(), Stderr: stderr.String()}
	result.Report = environment.newReport(previous)
	return result
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func (environment *e2eEnvironment) reportPaths() []string {
	matches, _ := filepath.Glob(filepath.Join(environment.output, ".music-ops", "runs", "*", "report.json"))
	return matches
}

func (environment *e2eEnvironment) newReport(previous []string) reportJSON {
	for _, path := range environment.reportPaths() {
		if slices.Contains(previous, path) {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			environment.t.Fatal(err)
		}
		var report reportJSON
		if err := json.Unmarshal(data, &report); err != nil {
			environment.t.Fatal(err)
		}
		return report
	}
	return reportJSON{}
}

func (environment *e2eEnvironment) addInput(fixtures ...fixture) {
	environment.t.Helper()
	writeFixtures(environment.t, environment.ffmpeg, environment.input, fixtures...)
}

func libraryFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".music-ops" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			relative, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func assertTree(t *testing.T, root string, expected ...string) {
	t.Helper()
	sort.Strings(expected)
	if diff := cmp.Diff(expected, libraryFiles(t, root), cmpopts.EquateEmpty()); diff != "" {
		t.Fatalf("library tree (-want +got):\n%s", diff)
	}
}

type expectedTrack struct {
	path        string
	title       string
	artist      string
	albumArtist string
	genre       string
	track       string
}

type probeOutput struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Profile     string            `json:"profile"`
		SampleRate  string            `json:"sample_rate"`
		Channels    int               `json:"channels"`
		BitRate     string            `json:"bit_rate"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

func (environment *e2eEnvironment) probe(path string) probeOutput {
	environment.t.Helper()
	output, err := exec.CommandContext(context.Background(), environment.ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		environment.t.Fatalf("probing %s: %v", path, err)
	}
	var probed probeOutput
	if err := json.Unmarshal(output, &probed); err != nil {
		environment.t.Fatal(err)
	}
	return probed
}

func (environment *e2eEnvironment) assertTags(expected ...expectedTrack) {
	environment.t.Helper()
	for _, track := range expected {
		tags := lowerKeys(environment.probe(filepath.Join(environment.output, filepath.FromSlash(track.path))).Format.Tags)
		actual := expectedTrack{path: track.path, title: tags["title"], artist: tags["artist"], albumArtist: tags["album_artist"], genre: tags["genre"], track: tags["track"]}
		if diff := cmp.Diff(track, actual, cmp.AllowUnexported(expectedTrack{})); diff != "" {
			environment.t.Errorf("tags of %s (-want +got):\n%s", track.path, diff)
		}
	}
}

func lowerKeys(tags map[string]string) map[string]string {
	lowered := map[string]string{}
	for key, value := range tags {
		lowered[strings.ToLower(key)] = value
	}
	return lowered
}

func (environment *e2eEnvironment) assertAudio(path string, minimumBitrate, channels int) {
	environment.t.Helper()
	probed := environment.probe(filepath.Join(environment.output, filepath.FromSlash(path)))
	for _, stream := range probed.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		bitrate, _ := strconv.Atoi(stream.BitRate)
		if stream.CodecName != "aac" || stream.Profile != "LC" || stream.SampleRate != "44100" || stream.Channels != channels || bitrate < minimumBitrate {
			environment.t.Errorf("%s audio = %+v", path, stream)
		}
		return
	}
	environment.t.Errorf("%s has no audio stream", path)
}

func snapshotHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return readErr
		}
		digest := sha256.Sum256(data)
		hashes[path] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}

func assertInputUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if diff := cmp.Diff(before, snapshotHashes(t, root)); diff != "" {
		t.Fatalf("input changed (-before +after):\n%s", diff)
	}
}

func TestE2E_Version(t *testing.T) {
	environment := newE2EEnvironment(t)

	result := environment.run("version")

	if result.ExitCode != 0 || !strings.HasPrefix(result.Stdout, "music-ops dev\nffmpeg ") || !strings.Contains(result.Stdout, "encoder ") {
		t.Fatalf("result = %+v", result)
	}
}

func TestE2E_HarnessIsolation(t *testing.T) {
	environment := newE2EEnvironment(t)

	result := environment.run("config", "path")

	expected := filepath.Join(environment.appData, "music-ops", "config.yaml")
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != expected {
		t.Fatalf("result = %+v want %s", result, expected)
	}
}

func TestE2E_FakeServicesAnswer(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.lastfm.setArtistTags("Queen", "rock")

	credits := environment.localOracle.answer(false, `{"items":[{"key":"t01","title":"Song (feat. Guest)","artist":"Main"}]}`)
	genres := environment.localOracle.answer(true, `{"items":[{"key":"t01","title":"x","tags":["rock"]}]}`)

	if credits != `{"items":[{"artists":["Main","Guest"],"key":"t01","title":"Song"}]}` || !strings.Contains(genres, `"genre":"Rock"`) {
		t.Fatalf("credits = %s genres = %s", credits, genres)
	}
}
