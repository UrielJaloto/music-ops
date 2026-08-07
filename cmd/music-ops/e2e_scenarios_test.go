//go:build e2e

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fdkMinimumBitrate    = 235520
	nativeMinimumBitrate = 64000
)

type encoderPath struct {
	name           string
	minimumBitrate int
	configure      func(environment *e2eEnvironment)
}

var encoderPaths = []encoderPath{
	{name: "FDK", minimumBitrate: fdkMinimumBitrate, configure: func(*e2eEnvironment) {}},
	{name: "Native", minimumBitrate: nativeMinimumBitrate, configure: func(environment *e2eEnvironment) { environment.useNativeEncoder() }},
}

func stereo(path, format string, tags fixtureTags) fixture {
	return fixture{path: path, format: format, tags: tags, sampleRate: 44100, channels: 2, bitDepth: 16, seconds: 2}
}

func TestE2E_HappyPath(t *testing.T) {
	for _, encoder := range encoderPaths {
		t.Run(encoder.name, func(t *testing.T) {
			environment := newE2EEnvironment(t)
			encoder.configure(environment)
			hires := stereo("Main - Album/02.flac", "flac", fixtureTags{title: "Song Two", artist: "$werve", album: "Album", track: 2, trackTotal: 3})
			hires.sampleRate, hires.bitDepth, hires.hasCover = 96000, 24, true
			environment.addInput(
				stereo("Main - Album/01.flac", "flac", fixtureTags{title: "HEATSEEKER (feat. Ethan Ross)", artist: "$werve", album: "Album", track: 1, trackTotal: 3}),
				hires,
				stereo("Main - Album/03.wav", "wav", fixtureTags{title: "Third (with A & B)", artist: "$werve"}),
			)
			environment.localOracle.setGenre("HEATSEEKER", "Electronic", "House", false)
			environment.localOracle.setGenre("Song Two", "Electronic", "", false)
			environment.localOracle.setGenre("Third", "Hip-Hop", "", false)
			before := snapshotHashes(t, environment.input)

			result := environment.run("run", environment.input)

			if result.ExitCode != 0 || result.Report.Counts["published"] != 3 || result.Report.Counts["failed"] != 0 {
				t.Fatalf("result = %+v", result)
			}
			assertTree(t, environment.output, "$werve - HEATSEEKER (feat. Ethan Ross).m4a", "$werve - Song Two.m4a", "$werve - Third (feat. A; B).m4a")
			environment.assertTags(
				expectedTrack{path: "$werve - HEATSEEKER (feat. Ethan Ross).m4a", title: "HEATSEEKER", artist: "$werve; Ethan Ross", albumArtist: "$werve", genre: "Electronic; House", track: "1/3"},
				expectedTrack{path: "$werve - Song Two.m4a", title: "Song Two", artist: "$werve", albumArtist: "$werve", genre: "Electronic", track: "2/3"},
				expectedTrack{path: "$werve - Third (feat. A; B).m4a", title: "Third", artist: "$werve; A; B", albumArtist: "$werve", genre: "Hip-Hop", track: "3/3"},
			)
			for _, file := range libraryFiles(t, environment.output) {
				environment.assertAudio(file, encoder.minimumBitrate, 2)
			}
			if !environment.hasAttachedCover("$werve - Song Two.m4a") {
				t.Fatal("cover missing")
			}
			assertInputUnchanged(t, environment.input, before)
		})
	}
}

func (environment *e2eEnvironment) hasAttachedCover(path string) bool {
	for _, stream := range environment.probe(filepath.Join(environment.output, path)).Streams {
		if stream.Disposition["attached_pic"] == 1 {
			return true
		}
	}
	return false
}

func TestE2E_MixedInputs(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.addInput(
		stereo("Mixed/aac.m4a", "m4a", fixtureTags{title: "Copied", artist: "Band"}),
		stereo("Mixed/lossy.mp3", "mp3", fixtureTags{title: "Encoded", artist: "Band"}),
	)
	writeInputFile(t, environment.input, "Mixed/cover.jpg", "jpeg")
	writeInputFile(t, environment.input, "Mixed/notes.txt", "notes")
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 2 || result.Report.Counts["skipped"] != 2 {
		t.Fatalf("result = %+v", result)
	}
	assertTree(t, environment.output, "Band - Copied.m4a", "Band - Encoded.m4a")
	environment.assertAudio("Band - Encoded.m4a", fdkMinimumBitrate, 2)
	copied := environment.probe(filepath.Join(environment.output, "Band - Copied.m4a"))
	if copied.Streams[0].CodecName != "aac" || copied.Streams[0].BitRate == "" || strings.HasPrefix(copied.Streams[0].BitRate, "25") {
		t.Fatalf("copied stream = %+v", copied.Streams[0])
	}
	assertInputUnchanged(t, environment.input, before)
}

func writeInputFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestE2E_Duplicates(t *testing.T) {
	for _, encoder := range encoderPaths {
		t.Run(encoder.name, func(t *testing.T) {
			environment := newE2EEnvironment(t)
			encoder.configure(environment)
			hires := stereo("B/01.flac", "flac", fixtureTags{title: "Same Song", artist: "Band"})
			hires.sampleRate, hires.bitDepth = 96000, 24
			environment.addInput(
				stereo("A/01.mp3", "mp3", fixtureTags{title: "Same Song", artist: "Band"}),
				hires,
				stereo("C/01.flac", "flac", fixtureTags{title: "Already There", artist: "Band"}),
			)
			writeInputFile(t, environment.output, "Band - Already There.m4a", "library copy")
			libraryBefore := snapshotHashes(t, environment.output)
			before := snapshotHashes(t, environment.input)

			result := environment.run("run", environment.input)

			if result.ExitCode != 0 || result.Report.Counts["published"] != 1 || result.Report.Counts["duplicate"] != 2 {
				t.Fatalf("result = %+v", result)
			}
			assertTree(t, environment.output, "Band - Already There.m4a", "Band - Same Song.m4a")
			if libraryCopy := libraryBefore[filepath.Join(environment.output, "Band - Already There.m4a")]; snapshotHashes(t, environment.output)[filepath.Join(environment.output, "Band - Already There.m4a")] != libraryCopy {
				t.Fatal("library file was modified")
			}
			if environment.probe(filepath.Join(environment.output, "Band - Same Song.m4a")).Format.Duration == "" || !reportHasPublished(result.Report, "B", "01.flac") {
				t.Fatalf("the lossless source should be the keeper: %+v", result.Report.Tracks)
			}
			assertInputUnchanged(t, environment.input, before)
		})
	}
}

func reportHasPublished(report reportJSON, folder, file string) bool {
	for _, track := range report.Tracks {
		if track.Status == "published" && strings.HasSuffix(filepath.ToSlash(track.Source), folder+"/"+file) {
			return true
		}
	}
	return false
}

func TestE2E_LongSourcePath(t *testing.T) {
	environment := newE2EEnvironment(t)
	deep := strings.Repeat("Very Long Folder Name ", 6) + "end/" + strings.Repeat("Another Long Folder ", 6) + "end/" + strings.Repeat("Track Name ", 6) + "end.flac"
	environment.addInput(stereo(deep, "flac", fixtureTags{title: "Deep Track", artist: "Band"}))
	if source := filepath.Join(environment.input, filepath.FromSlash(deep)); len(source) <= 260 {
		t.Fatalf("source path is only %d characters", len(source))
	}
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 1 {
		t.Fatalf("result = %+v", result)
	}
	assertTree(t, environment.output, "Band - Deep Track.m4a")
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_UnicodeNamesAndTags(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.addInput(
		stereo("¥$ - Vultures/01.flac", "flac", fixtureTags{title: "PAPERWORK (feat. Ty Dolla $ign)", artist: "¥$"}),
		stereo("Motörhead/Ace.flac", "flac", fixtureTags{title: "Ace of Spades 🂡", artist: "Motörhead"}),
		stereo("1Kilo/Benção.flac", "flac", fixtureTags{title: "Benção", artist: "1Kilo"}),
	)
	environment.localOracle.setGenre("Benção", "Rap", "", true)
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 3 {
		t.Fatalf("result = %+v", result)
	}
	assertTree(t, environment.output, "¥$ - PAPERWORK (feat. Ty Dolla $ign).m4a", "Motörhead - Ace of Spades 🂡.m4a", "1Kilo - Benção.m4a")
	environment.assertTags(
		expectedTrack{path: "¥$ - PAPERWORK (feat. Ty Dolla $ign).m4a", title: "PAPERWORK", artist: "¥$; Ty Dolla $ign", albumArtist: "¥$", genre: "desconhecido", track: "1/1"},
		expectedTrack{path: "Motörhead - Ace of Spades 🂡.m4a", title: "Ace of Spades 🂡", artist: "Motörhead", albumArtist: "Motörhead", genre: "desconhecido", track: "1/1"},
		expectedTrack{path: "1Kilo - Benção.m4a", title: "Benção", artist: "1Kilo", albumArtist: "1Kilo", genre: "Rap Nacional", track: "1/1"},
	)
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_CorruptFile(t *testing.T) {
	environment := newE2EEnvironment(t)
	broken := stereo("Album/02.flac", "flac", fixtureTags{title: "Broken", artist: "Band"})
	broken.isCorrupt = true
	environment.addInput(stereo("Album/01.flac", "flac", fixtureTags{title: "Fine", artist: "Band"}), broken)
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 2 || result.Report.Counts["published"] != 1 || result.Report.Counts["failed"] != 1 {
		t.Fatalf("result = %+v", result)
	}
	assertTree(t, environment.output, "Band - Fine.m4a")
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_NoAdvisorAvailable(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.llama.server.Close()
	environment.lastfm.setTrackTags("Band", "Rocker", "rock")
	environment.addInput(stereo("Album/01.flac", "flac", fixtureTags{title: "Rocker (feat. Guest)", artist: "Band"}))
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 1 || len(result.Report.Warnings) == 0 {
		t.Fatalf("result = %+v", result)
	}
	environment.assertTags(expectedTrack{path: "Band - Rocker (feat. Guest).m4a", title: "Rocker", artist: "Band; Guest", albumArtist: "Band", genre: "Rock", track: "1/1"})
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_AdvisorInventsArtist(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.localOracle.setMode(oracleInventsArtist)
	environment.addInput(stereo("Album/01.flac", "flac", fixtureTags{title: "Hit (feat. Guest)", artist: "Band"}))
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || environment.llama.calls.Load() == 0 || !hasWarning(result.Report, "credit rules used") {
		t.Fatalf("result = %+v llama calls = %d", result, environment.llama.calls.Load())
	}
	environment.assertTags(expectedTrack{path: "Band - Hit (feat. Guest).m4a", title: "Hit", artist: "Band; Guest", albumArtist: "Band", genre: "desconhecido", track: "1/1"})
	assertInputUnchanged(t, environment.input, before)
}

func hasWarning(report reportJSON, fragment string) bool {
	for _, warning := range report.Warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func TestE2E_LastFMAlwaysFailing(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.lastfm.isFailing = true
	environment.localOracle.setGenre("Known Song", "Pop", "", false)
	environment.addInput(
		stereo("Album/01.flac", "flac", fixtureTags{title: "Known Song", artist: "Band"}),
		stereo("Album/02.flac", "flac", fixtureTags{title: "Unknown Song", artist: "Band"}),
	)
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 2 {
		t.Fatalf("result = %+v", result)
	}
	environment.assertTags(
		expectedTrack{path: "Band - Known Song.m4a", title: "Known Song", artist: "Band", albumArtist: "Band", genre: "Pop", track: "1/2"},
		expectedTrack{path: "Band - Unknown Song.m4a", title: "Unknown Song", artist: "Band", albumArtist: "Band", genre: "desconhecido", track: "2/2"},
	)
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_SecondRunWritesNothing(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.lastfm.setTrackTags("Band", "Again", "rock")
	environment.addInput(stereo("Album/01.flac", "flac", fixtureTags{title: "Again", artist: "Band"}))
	before := snapshotHashes(t, environment.input)
	first := environment.run("run", environment.input)
	libraryAfterFirst := snapshotHashes(t, environment.output+"/Band - Again.m4a")
	lastfmCalls, llamaCalls := environment.lastfm.calls.Load(), environment.llama.calls.Load()

	second := environment.run("run", environment.input)

	if first.ExitCode != 0 || second.ExitCode != 0 || second.Report.Counts["published"] != 0 || second.Report.Counts["skipped"] != 1 {
		t.Fatalf("first = %+v second = %+v", first.Report, second.Report)
	}
	if environment.lastfm.calls.Load() != lastfmCalls || environment.llama.calls.Load() != llamaCalls {
		t.Fatalf("network calls on second run: last.fm %d→%d llama %d→%d", lastfmCalls, environment.lastfm.calls.Load(), llamaCalls, environment.llama.calls.Load())
	}
	assertInputUnchanged(t, environment.output+"/Band - Again.m4a", libraryAfterFirst)
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_CrashThenResume(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.addInput(
		stereo("Album/01.flac", "flac", fixtureTags{title: "One", artist: "Band"}),
		stereo("Album/02.flac", "flac", fixtureTags{title: "Two", artist: "Band"}),
		stereo("Album/03.flac", "flac", fixtureTags{title: "Three", artist: "Band"}),
	)
	before := snapshotHashes(t, environment.input)
	environment.extraEnv = []string{"MUSIC_OPS_TEST_CRASH_AFTER=1"}
	crashed := environment.run("run", environment.input)
	environment.extraEnv = nil

	resumed := environment.run("run", environment.input)

	if crashed.ExitCode == 0 || resumed.ExitCode != 0 {
		t.Fatalf("crashed = %d %s resumed = %+v", crashed.ExitCode, crashed.Stderr, resumed)
	}
	assertTree(t, environment.output, "Band - One.m4a", "Band - Three.m4a", "Band - Two.m4a")
	assertNoLeftovers(t, environment.output)
	assertInputUnchanged(t, environment.input, before)
}

func assertNoLeftovers(t *testing.T, output string) {
	t.Helper()
	leftovers, _ := filepath.Glob(filepath.Join(output, "*.tmp"))
	staged, _ := filepath.Glob(filepath.Join(output, ".music-ops", "staging", "*", "*"))
	if len(leftovers) != 0 || len(staged) != 0 {
		t.Fatalf("leftovers = %v staged = %v", leftovers, staged)
	}
}

func TestE2E_PreviewLeavesOutputUntouched(t *testing.T) {
	environment := newE2EEnvironment(t)
	environment.addInput(stereo("Album/01.flac", "flac", fixtureTags{title: "Planned (feat. Guest)", artist: "Band"}))
	before := snapshotHashes(t, environment.input)

	result := environment.run("preview", environment.input)

	if result.ExitCode != 0 || !strings.Contains(result.Stdout, "Band - Planned (feat. Guest).m4a") || !strings.Contains(result.Stdout, "1 tracks planned") {
		t.Fatalf("result = %+v", result)
	}
	assertTree(t, environment.output)
	assertInputUnchanged(t, environment.input, before)
}

func TestE2E_Cancellation(t *testing.T) {
	environment := newE2EEnvironment(t)
	fixtures := []fixture{}
	for index := range 12 {
		long := stereo("Album/"+string(rune('a'+index))+".flac", "flac", fixtureTags{title: "Track " + string(rune('A'+index)), artist: "Band"})
		long.seconds = 20
		fixtures = append(fixtures, long)
	}
	environment.addInput(fixtures...)
	before := snapshotHashes(t, environment.input)
	environment.extraEnv = []string{"MUSIC_OPS_TEST_CANCEL_AFTER_MS=1500"}

	result := environment.run("run", environment.input)

	if result.ExitCode != 130 || !bytes.Contains([]byte(result.Stderr), []byte("cancelled")) {
		t.Fatalf("result = %+v", result)
	}
	assertNoLeftovers(t, environment.output)
	assertInputUnchanged(t, environment.input, before)
}
