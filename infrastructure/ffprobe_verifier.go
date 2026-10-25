package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	publishedCodec         = "aac"
	publishedSampleRate    = 44100
	bitrateTolerance       = 0.08
	durationTolerance      = 100 * time.Millisecond
	verifiedFieldSeparator = ", "
)

var (
	ErrPublishedAudioMismatch = errors.New("published audio does not match")
	ErrAudioStreamMissing     = errors.New("audio stream is missing")
)

var writtenTagOrder = []string{"title", "artist", "album_artist", "album", "genre", "track", "disc", "date", "composer", "copyright"}

type FFprobeVerifier struct {
	inspector FFprobeInspector
}

func NewFFprobeVerifier(inspector FFprobeInspector) FFprobeVerifier {
	return FFprobeVerifier{inspector: inspector}
}

func (verifier FFprobeVerifier) Verify(ctx context.Context, path string, expected services.PublishedAudio) error {
	record, err := verifier.inspector.probe(ctx, path)
	if err != nil {
		return err
	}
	stream, err := firstAudioStreamOf(record)
	if err != nil {
		return err
	}
	mismatches := audioMismatches(stream, record.Format, expected)
	mismatches = append(mismatches, tagMismatches(lowerCaseTags(record.Format.Tags, stream.Tags), expected)...)
	if len(mismatches) > 0 {
		return fmt.Errorf("%w: %s", ErrPublishedAudioMismatch, strings.Join(mismatches, verifiedFieldSeparator))
	}
	return nil
}

func audioMismatches(stream probeStreamRecord, format probeFormatRecord, expected services.PublishedAudio) []string {
	quality := toQualityDetails(stream, format)
	mismatches := []string{}
	if stream.CodecName != publishedCodec {
		mismatches = append(mismatches, "codec "+stream.CodecName)
	}
	if quality.SampleRate != publishedSampleRate {
		mismatches = append(mismatches, "sample rate "+strconv.Itoa(quality.SampleRate))
	}
	if expected.IsReencoded && !isBitrateWithinTolerance(quality.BitRate, expected.Bitrate) {
		mismatches = append(mismatches, "bitrate "+strconv.Itoa(quality.BitRate))
	}
	if (quality.Duration - expected.Duration).Abs() > durationTolerance {
		mismatches = append(mismatches, "duration "+quality.Duration.String())
	}
	return mismatches
}

func isBitrateWithinTolerance(actual, expected int) bool {
	return math.Abs(float64(actual-expected)) <= float64(expected)*bitrateTolerance
}

func tagMismatches(tags map[string]string, expected services.PublishedAudio) []string {
	metadata := expected.Metadata
	written := map[string]string{
		"title": metadata.Title, "artist": metadata.Artist, "album_artist": metadata.AlbumArtist,
		"album": metadata.Album, "genre": metadata.Genre, "track": metadata.Track, "disc": metadata.Disc,
		"date": metadata.Date, "composer": metadata.Composer, "copyright": metadata.Copyright,
	}
	mismatches := []string{}
	for _, key := range writtenTagOrder {
		if written[key] != "" && tags[key] != written[key] {
			mismatches = append(mismatches, "tag "+key)
		}
	}
	return mismatches
}

func lowerCaseTags(formatTags, streamTags map[string]string) map[string]string {
	merged := map[string]string{}
	addLowerCaseKeys(merged, formatTags)
	addLowerCaseKeys(merged, streamTags)
	return merged
}

func addLowerCaseKeys(target, source map[string]string) {
	for key, value := range source {
		target[strings.ToLower(key)] = value
	}
}
