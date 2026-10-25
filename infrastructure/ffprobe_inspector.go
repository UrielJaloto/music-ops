package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

const (
	audioCodecType     = "audio"
	coverAbsent        = -1
	longPathLimit      = 240
	longPathPrefix     = `\\?\`
	positionSeparator  = "/"
	attachedPicFlagSet = 1
	missingProbeValue  = "N/A"
)

var sampleFormatBits = map[string]int{
	"u8": 8, "u8p": 8, "s16": 16, "s16p": 16, "s32": 32, "s32p": 32,
	"flt": 32, "fltp": 32, "dbl": 64, "dblp": 64, "s64": 64, "s64p": 64,
}

var tagAliases = map[string]string{
	"title": "title", "artist": "artist", "album_artist": "album_artist", "albumartist": "album_artist",
	"album artist": "album_artist", "album": "album", "date": "date", "year": "date", "composer": "composer",
	"copyright": "copyright", "track": "track", "tracknumber": "track", "tracktotal": "tracktotal",
	"totaltracks": "tracktotal", "disc": "disc", "discnumber": "disc", "disctotal": "disctotal", "totaldiscs": "disctotal",
}

type ProcessExecutor interface {
	Run(ctx context.Context, request ProcessRequest) (ProcessResult, error)
}

type FFprobeInspector struct {
	runner     ProcessExecutor
	executable string
}

func NewFFprobeInspector(runner ProcessExecutor, executable string) FFprobeInspector {
	return FFprobeInspector{runner: runner, executable: executable}
}

type probeRecord struct {
	Streams []probeStreamRecord `json:"streams"`
	Format  probeFormatRecord   `json:"format"`
}

type probeStreamRecord struct {
	Index            int                    `json:"index"`
	CodecName        string                 `json:"codec_name"`
	CodecType        string                 `json:"codec_type"`
	SampleFormat     string                 `json:"sample_fmt"`
	SampleRate       string                 `json:"sample_rate"`
	Channels         int                    `json:"channels"`
	BitsPerRawSample string                 `json:"bits_per_raw_sample"`
	BitsPerSample    int                    `json:"bits_per_sample"`
	BitRate          string                 `json:"bit_rate"`
	Duration         string                 `json:"duration"`
	Disposition      probeDispositionRecord `json:"disposition"`
	Tags             map[string]string      `json:"tags"`
}

type probeDispositionRecord struct {
	AttachedPic int `json:"attached_pic"`
}

type probeFormatRecord struct {
	BitRate  string            `json:"bit_rate"`
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
}

func (inspector FFprobeInspector) Inspect(ctx context.Context, path string) (services.InspectedAudio, error) {
	record, err := inspector.probe(ctx, path)
	if err != nil {
		return services.InspectedAudio{}, err
	}
	return toInspectedAudio(record)
}

func (inspector FFprobeInspector) probe(ctx context.Context, path string) (probeRecord, error) {
	var stdout bytes.Buffer
	request := ProcessRequest{
		Executable: inspector.executable,
		Arguments:  []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", processPath(path)},
		Stdout:     &stdout,
	}
	if _, err := inspector.runner.Run(ctx, request); err != nil {
		return probeRecord{}, fmt.Errorf("probing %s: %w", path, err)
	}
	return decodeProbe(stdout.Bytes())
}

func decodeProbe(data []byte) (probeRecord, error) {
	var record probeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return probeRecord{}, fmt.Errorf("decoding ffprobe output: %w", err)
	}
	return record, nil
}

func processPath(path string) string {
	if len(path) <= longPathLimit || strings.HasPrefix(path, longPathPrefix) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return longPathPrefix + absolute
}

func toInspectedAudio(record probeRecord) (services.InspectedAudio, error) {
	stream, err := firstAudioStreamOf(record)
	if err != nil {
		return services.InspectedAudio{}, err
	}
	return services.InspectedAudio{
		Codec:   stream.CodecName,
		Quality: toQualityDetails(stream, record.Format),
		Tags:    toSourceTags(mergedTags(record.Format.Tags, stream.Tags)),
		Cover:   coverOf(record.Streams),
	}, nil
}

func firstAudioStreamOf(record probeRecord) (probeStreamRecord, error) {
	for _, stream := range record.Streams {
		if stream.CodecType == audioCodecType {
			return stream, nil
		}
	}
	return probeStreamRecord{}, ErrAudioStreamMissing
}

func toQualityDetails(stream probeStreamRecord, format probeFormatRecord) domain.AudioQualityDetails {
	return domain.AudioQualityDetails{
		SampleRate: atoiOrZero(stream.SampleRate),
		BitDepth:   bitDepthOf(stream),
		Channels:   stream.Channels,
		BitRate:    atoiOrZero(firstPresent(stream.BitRate, format.BitRate)),
		Duration:   secondsToDuration(firstPresent(stream.Duration, format.Duration)),
	}
}

func bitDepthOf(stream probeStreamRecord) int {
	if bits := atoiOrZero(stream.BitsPerRawSample); bits > 0 {
		return bits
	}
	if stream.BitsPerSample > 0 {
		return stream.BitsPerSample
	}
	return sampleFormatBits[stream.SampleFormat]
}

func coverOf(streams []probeStreamRecord) domain.CoverArt {
	for _, stream := range streams {
		if stream.Disposition.AttachedPic == attachedPicFlagSet {
			return domain.CoverArt{Stream: stream.Index, Codec: stream.CodecName}
		}
	}
	return domain.CoverArt{Stream: coverAbsent}
}

func mergedTags(formatTags, streamTags map[string]string) map[string]string {
	merged := map[string]string{}
	addAliasedTags(merged, formatTags)
	addAliasedTags(merged, streamTags)
	return merged
}

func addAliasedTags(target, source map[string]string) {
	for key, value := range source {
		if name, isKnown := tagAliases[strings.ToLower(key)]; isKnown {
			target[name] = norm.NFC.String(strings.TrimSpace(value))
		}
	}
}

func toSourceTags(tags map[string]string) domain.SourceTags {
	trackNumber, trackTotal := splitPosition(tags["track"])
	discNumber, discTotal := splitPosition(tags["disc"])
	return domain.SourceTags{
		Title:       tags["title"],
		Artist:      tags["artist"],
		AlbumArtist: tags["album_artist"],
		Album:       tags["album"],
		Date:        tags["date"],
		Composer:    tags["composer"],
		Copyright:   tags["copyright"],
		Position: domain.SourcePosition{
			TrackNumber: trackNumber,
			TrackTotal:  max(trackTotal, atoiOrZero(tags["tracktotal"])),
			DiscNumber:  discNumber,
			DiscTotal:   max(discTotal, atoiOrZero(tags["disctotal"])),
		},
	}
}

func splitPosition(value string) (int, int) {
	number, total, _ := strings.Cut(value, positionSeparator)
	return atoiOrZero(strings.TrimSpace(number)), atoiOrZero(strings.TrimSpace(total))
}

func firstPresent(values ...string) string {
	for _, value := range values {
		if value != "" && value != missingProbeValue {
			return value
		}
	}
	return ""
}

func secondsToDuration(value string) time.Duration {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
