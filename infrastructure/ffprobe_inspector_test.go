package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type ffprobeRunnerStub struct {
	output    []byte
	err       error
	arguments []string
}

func (stub *ffprobeRunnerStub) Run(_ context.Context, request ProcessRequest) (ProcessResult, error) {
	stub.arguments = request.Arguments
	if stub.err != nil {
		return ProcessResult{}, stub.err
	}
	_, err := request.Stdout.Write(stub.output)
	return ProcessResult{}, err
}

func ffprobeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ffprobe", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFFprobeInspector_Inspect(t *testing.T) {
	cases := []struct {
		fixture  string
		expected services.InspectedAudio
	}{
		{fixture: "flac_vorbis.json", expected: services.InspectedAudio{
			Codec:   "flac",
			Quality: domain.AudioQualityDetails{SampleRate: 96000, BitDepth: 24, Channels: 2, BitRate: 2950000, Duration: 215400 * time.Millisecond},
			Tags: domain.SourceTags{
				Title: "Bencao (feat. Guest)", Artist: "Main Artist", AlbumArtist: "Main Artist", Album: "The Album", Date: "2019",
				Composer: "Writer", Copyright: "2019 Label", Position: domain.SourcePosition{TrackNumber: 3, TrackTotal: 12, DiscNumber: 1, DiscTotal: 2},
			},
			Cover: domain.CoverArt{Stream: 1, Codec: "png"},
		}},
		{fixture: "freac_m4a.json", expected: services.InspectedAudio{
			Codec:   "aac",
			Quality: domain.AudioQualityDetails{SampleRate: 44100, BitDepth: 32, Channels: 2, BitRate: 256000, Duration: 180023220 * time.Microsecond},
			Tags: domain.SourceTags{
				Title: "Song", Artist: "Artist", AlbumArtist: "Artist", Album: "Favorites", Date: "2024",
				Position: domain.SourcePosition{TrackNumber: 5, TrackTotal: 50, DiscNumber: 1, DiscTotal: 1},
			},
			Cover: domain.CoverArt{Stream: 1, Codec: "mjpeg"},
		}},
		{fixture: "mp3_id3.json", expected: services.InspectedAudio{
			Codec:   "mp3",
			Quality: domain.AudioQualityDetails{SampleRate: 48000, BitDepth: 32, Channels: 2, BitRate: 320000, Duration: 200500 * time.Millisecond},
			Tags: domain.SourceTags{
				Title: "Mp3 Song", Artist: "Mp3 Artist", AlbumArtist: "Various", Album: "Hits", Date: "2001",
				Position: domain.SourcePosition{TrackNumber: 7, TrackTotal: 15, DiscNumber: 2, DiscTotal: 3},
			},
			Cover: domain.CoverArt{Stream: 1, Codec: "mjpeg"},
		}},
		{fixture: "opus_stream_tags.json", expected: services.InspectedAudio{
			Codec:   "opus",
			Quality: domain.AudioQualityDetails{SampleRate: 48000, BitDepth: 32, Channels: 2, BitRate: 128000, Duration: 61 * time.Second},
			Tags:    domain.SourceTags{Title: "Stream Title", Artist: "Stream Artist", Date: "2020", Position: domain.SourcePosition{TrackNumber: 2, TrackTotal: 9}},
			Cover:   domain.CoverArt{Stream: -1},
		}},
		{fixture: "wav_no_tags.json", expected: services.InspectedAudio{
			Codec:   "pcm_s16le",
			Quality: domain.AudioQualityDetails{SampleRate: 44100, BitDepth: 16, Channels: 1, BitRate: 705600, Duration: time.Second},
			Cover:   domain.CoverArt{Stream: -1},
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.fixture, func(t *testing.T) {
			runner := &ffprobeRunnerStub{output: ffprobeFixture(t, testCase.fixture)}

			inspected, err := NewFFprobeInspector(runner, "ffprobe").Inspect(context.Background(), "song.flac")

			if diff := cmp.Diff(testCase.expected, inspected); diff != "" || err != nil {
				t.Fatalf("inspected (-want +got):\n%s err = %v", diff, err)
			}
		})
	}
}

func TestFFprobeInspector_Inspect_NFCTags(t *testing.T) {
	runner := &ffprobeRunnerStub{output: []byte(`{"streams":[{"codec_type":"audio","codec_name":"flac","tags":{"title":"Benc\u0327a\u0303o"}}],"format":{}}`)}

	inspected, err := NewFFprobeInspector(runner, "ffprobe").Inspect(context.Background(), "a.flac")

	if err != nil || inspected.Tags.Title != "Ben\u00e7\u00e3o" {
		t.Fatalf("title = %q err = %v", inspected.Tags.Title, err)
	}
}

func TestFFprobeInspector_Inspect_Errors(t *testing.T) {
	errProcess := errors.New("ffprobe crashed")
	cases := []struct {
		name          string
		runner        *ffprobeRunnerStub
		expectedError error
	}{
		{name: "ProcessFails", runner: &ffprobeRunnerStub{err: errProcess}, expectedError: errProcess},
		{name: "NoAudioStream", runner: &ffprobeRunnerStub{output: []byte(`{"streams":[{"codec_type":"video"}],"format":{}}`)}, expectedError: ErrAudioStreamMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewFFprobeInspector(testCase.runner, "ffprobe").Inspect(context.Background(), "a.flac")

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFFprobeInspector_Inspect_MalformedJSON(t *testing.T) {
	_, err := NewFFprobeInspector(&ffprobeRunnerStub{output: []byte("{")}, "ffprobe").Inspect(context.Background(), "a.flac")

	if err == nil || !strings.Contains(err.Error(), "decoding ffprobe output") {
		t.Fatalf("err = %v", err)
	}
}

func TestFFprobeInspector_LongPathsUsePrefix(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		expected func(path string) string
	}{
		{name: "AtLimit_Unchanged", path: `C:\` + strings.Repeat("a", 237), expected: func(path string) string { return path }},
		{name: "AboveLimit_Prefixed", path: `C:\` + strings.Repeat("a", 238), expected: func(path string) string { return longPathPrefix + path }},
		{name: "AlreadyPrefixed_Unchanged", path: longPathPrefix + `C:\` + strings.Repeat("a", 300), expected: func(path string) string { return path }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &ffprobeRunnerStub{output: ffprobeFixture(t, "wav_no_tags.json")}

			_, err := NewFFprobeInspector(runner, "ffprobe").Inspect(context.Background(), testCase.path)

			if err != nil || runner.arguments[len(runner.arguments)-1] != testCase.expected(testCase.path) {
				t.Fatalf("arguments = %v err = %v", runner.arguments, err)
			}
		})
	}
}

func publishedProbe(bitRate, duration string, tags string) []byte {
	return []byte(`{"streams":[{"codec_type":"audio","codec_name":"aac","sample_rate":"44100","channels":2,"bit_rate":"` + bitRate + `","duration":"` + duration + `"}],"format":{"tags":{` + tags + `}}}`)
}

func TestFFprobeVerifier_Verify(t *testing.T) {
	metadata := domain.TrackMetadata{Title: "Song", Artist: "Artist", Genre: "Rock", Track: "1/2"}
	matchingTags := `"title":"Song","ARTIST":"Artist","genre":"Rock","track":"1/2"`
	expected := services.PublishedAudio{Metadata: metadata, Duration: 3 * time.Second, Bitrate: 256000, IsReencoded: true}
	cases := []struct {
		name     string
		probe    []byte
		expected services.PublishedAudio
		mismatch string
	}{
		{name: "Matching", probe: publishedProbe("256000", "3.000000", matchingTags), expected: expected},
		{name: "BitrateAtLowerTolerance", probe: publishedProbe("235520", "3.0", matchingTags), expected: expected},
		{name: "BitrateBelowLowerTolerance", probe: publishedProbe("235519", "3.0", matchingTags), expected: expected, mismatch: "bitrate 235519"},
		{name: "BitrateAtUpperTolerance", probe: publishedProbe("276480", "3.0", matchingTags), expected: expected},
		{name: "BitrateAboveUpperTolerance", probe: publishedProbe("276481", "3.0", matchingTags), expected: expected, mismatch: "bitrate 276481"},
		{name: "BitrateIgnoredWhenStreamCopied", probe: publishedProbe("128000", "3.0", matchingTags), expected: services.PublishedAudio{Metadata: metadata, Duration: 3 * time.Second}},
		{name: "DurationAtTolerance", probe: publishedProbe("256000", "3.1", matchingTags), expected: expected},
		{name: "DurationAboveTolerance", probe: publishedProbe("256000", "3.101", matchingTags), expected: expected, mismatch: "duration 3.101s"},
		{name: "DurationBelowTolerance", probe: publishedProbe("256000", "2.899", matchingTags), expected: expected, mismatch: "duration 2.899s"},
		{name: "TagDiffers", probe: publishedProbe("256000", "3.0", `"title":"Other","artist":"Artist","genre":"Rock","track":"1/2"`), expected: expected, mismatch: "tag title"},
		{name: "TagMissing", probe: publishedProbe("256000", "3.0", `"title":"Song","artist":"Artist","genre":"Rock"`), expected: expected, mismatch: "tag track"},
		{name: "WrongCodecAndRate", probe: []byte(`{"streams":[{"codec_type":"audio","codec_name":"mp3","sample_rate":"48000","bit_rate":"256000","duration":"3.0"}],"format":{"tags":{` + matchingTags + `}}}`), expected: expected, mismatch: "codec mp3, sample rate 48000"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := NewFFprobeVerifier(NewFFprobeInspector(&ffprobeRunnerStub{output: testCase.probe}, "ffprobe"))

			err := verifier.Verify(context.Background(), "out.m4a", testCase.expected)

			if testCase.mismatch == "" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if testCase.mismatch != "" && (!errors.Is(err, ErrPublishedAudioMismatch) || !strings.HasSuffix(err.Error(), ": "+testCase.mismatch)) {
				t.Fatalf("err = %v, want mismatch %q", err, testCase.mismatch)
			}
		})
	}
}

func TestFFprobeVerifier_Verify_Errors(t *testing.T) {
	errProcess := errors.New("ffprobe crashed")
	cases := []struct {
		name          string
		runner        *ffprobeRunnerStub
		expectedError error
	}{
		{name: "ProcessFails", runner: &ffprobeRunnerStub{err: errProcess}, expectedError: errProcess},
		{name: "NoAudioStream", runner: &ffprobeRunnerStub{output: []byte(`{"streams":[],"format":{}}`)}, expectedError: ErrAudioStreamMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := NewFFprobeVerifier(NewFFprobeInspector(testCase.runner, "ffprobe")).Verify(context.Background(), "a.m4a", services.PublishedAudio{})

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
