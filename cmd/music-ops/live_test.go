//go:build live

package main

import (
	"context"
	"os"
	"testing"

	"github.com/UrielJaloto/music-ops/infrastructure"
)

const liveLastFMURL = "https://ws.audioscrobbler.com/2.0/"

func liveAdvisor(t *testing.T) infrastructure.AdvisorConfig {
	t.Helper()
	paths := infrastructure.ReadEnvironmentPaths(os.LookupEnv)
	config, err := infrastructure.NewConfigStore(infrastructure.DefaultConfigPath(paths.AppData), os.LookupEnv).Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config.Advisor.LlamaServer, config.Advisor.ModelFile} {
		if _, err := os.Stat(path); path == "" || err != nil {
			t.Fatalf("local advisor is not installed (run music-ops advisor install): %q", path)
		}
	}
	return config.Advisor
}

func TestLive_RealServices(t *testing.T) {
	key, _ := os.LookupEnv("MUSIC_OPS_LASTFM_API_KEY")
	if key == "" {
		t.Fatal("MUSIC_OPS_LASTFM_API_KEY is required")
	}
	advisor := liveAdvisor(t)
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
	environment.assertTags(
		expectedTrack{path: "Queen - Bohemian Rhapsody.m4a", title: "Bohemian Rhapsody", artist: "Queen", albumArtist: "Queen", genre: "Rock", track: "1/1"},
		expectedTrack{path: "Racionais MC's - Negro Drama.m4a", title: "Negro Drama", artist: "Racionais MC's", albumArtist: "Racionais MC's", genre: "Rap Nacional", track: "1/1"},
		expectedTrack{path: "Alex Gaudino - Destination Calabria [Radio Edit] (feat. Crystal Waters).m4a", title: "Destination Calabria [Radio Edit]", artist: "Alex Gaudino; Crystal Waters", albumArtist: "Alex Gaudino", genre: "Electronic; Dance", track: "1/1"},
	)
	assertInputUnchanged(t, environment.input, before)
}
