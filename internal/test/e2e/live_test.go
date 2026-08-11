//go:build live

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/infrastructure/config"
)

const liveLastFMURL = "https://ws.audioscrobbler.com/2.0/"

func liveConfig(t *testing.T) config.Config {
	t.Helper()
	paths := config.ReadEnvironmentPaths(os.LookupEnv)
	loaded, err := config.NewConfigStore(config.DefaultConfigPath(paths.AppData), os.LookupEnv).Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{loaded.Advisor.LlamaServer, loaded.Advisor.ModelFile} {
		if _, err := os.Stat(path); path == "" || err != nil {
			t.Fatalf("local advisor is not installed (run music-ops advisor install): %q", path)
		}
	}
	if loaded.LastFM.APIKey == "" {
		t.Fatal("lastfm.api_key is not set (run music-ops config set lastfm.api_key <key>)")
	}
	return loaded
}

func TestLive_RealServices(t *testing.T) {
	config := liveConfig(t)
	key, advisor := config.LastFM.APIKey, config.Advisor
	environment := newE2EEnvironment(t)
	environment.settings["lastfm.api_key"] = key
	environment.settings["lastfm.base_url"] = liveLastFMURL
	environment.settings["advisor.llama_server"] = advisor.LlamaServer
	environment.settings["advisor.model_file"] = advisor.ModelFile
	environment.settings["advisor.port"] = "8089"
	environment.addInput(
		stereo("Queen/01.flac", "flac", fixtureTags{title: "Bohemian Rhapsody", artist: "Queen"}),
		stereo("Racionais/01.flac", "flac", fixtureTags{title: "Negro Drama", artist: "Racionais MC's"}),
		stereo("Gaudino/01.flac", "flac", fixtureTags{title: "Destination Calabria (feat. Crystal Waters) [Radio Edit]", artist: "Alex Gaudino"}),
	)
	before := snapshotHashes(t, environment.input)

	result := environment.run("run", environment.input)

	if result.ExitCode != 0 || result.Report.Counts["published"] != 3 {
		t.Fatalf("result = %+v", result)
	}
	environment.assertLiveTags(
		liveTrack{expectedTrack: expectedTrack{path: "in/Queen - Bohemian Rhapsody.m4a", title: "Bohemian Rhapsody", artist: "Queen", albumArtist: "Queen", track: "1/1"}},
		liveTrack{expectedTrack: expectedTrack{path: "in/Racionais MC's - Negro Drama.m4a", title: "Negro Drama", artist: "Racionais MC's", albumArtist: "Racionais MC's", track: "1/1"}, isBrazilian: true},
		liveTrack{expectedTrack: expectedTrack{path: "in/Alex Gaudino - Destination Calabria [Radio Edit] (feat. Crystal Waters).m4a", title: "Destination Calabria [Radio Edit]", artist: "Alex Gaudino; Crystal Waters", albumArtist: "Alex Gaudino", track: "1/1"}},
	)
	assertInputUnchanged(t, environment.input, before)
}

type liveTrack struct {
	expectedTrack
	isBrazilian bool
}

func (environment *e2eEnvironment) assertLiveTags(expected ...liveTrack) {
	environment.t.Helper()
	for _, track := range expected {
		tags := lowerKeys(environment.probe(filepath.Join(environment.output, filepath.FromSlash(track.path))).Format.Tags)
		genre := tags["genre"]
		withGenre := track.expectedTrack
		withGenre.genre = genre
		environment.assertTags(withGenre)
		if genre == "" || genre == "desconhecido" || strings.HasSuffix(genre, " "+genres.NationalSuffix) != track.isBrazilian {
			environment.t.Errorf("genre of %s = %q, brazilian = %v", track.path, genre, track.isBrazilian)
		}
	}
}
