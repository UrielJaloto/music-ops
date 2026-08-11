package transcoding

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
)

var copyableCoverCodecs = []string{"png", "mjpeg"}

var tagLineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

type FFmpegFinalizer struct {
	runner     ProcessExecutor
	executable string
}

func NewFFmpegFinalizer(runner ProcessExecutor, executable string) FFmpegFinalizer {
	return FFmpegFinalizer{runner: runner, executable: executable}
}

func (finalizer FFmpegFinalizer) Finalize(ctx context.Context, request publishing.FinalizeRequest) error {
	return runToPartial(ctx, finalizer.runner, finalizer.executable, buildFinalizeArguments(request), request.Destination)
}

func buildFinalizeArguments(request publishing.FinalizeRequest) []string {
	hasCover := request.CoverSource != "" && request.Cover.Stream >= 0
	arguments := []string{"-hide_banner", "-nostdin", "-y", "-i", toProcessPath(request.Staged)}
	if hasCover {
		arguments = append(arguments, "-i", toProcessPath(request.CoverSource))
	}
	arguments = append(arguments, "-map", "0:a:0")
	if hasCover {
		arguments = append(arguments, buildCoverArguments(request.Cover)...)
	} else {
		arguments = append(arguments, "-c", "copy")
	}
	arguments = append(arguments, "-map_metadata", "-1")
	arguments = append(arguments, buildMetadataArguments(request.Metadata)...)
	return append(arguments, "-f", "mp4", "-movflags", "+faststart", toProcessPath(request.Destination+partialSuffix))
}

func buildCoverArguments(cover tracks.CoverArt) []string {
	arguments := []string{"-map", "1:" + strconv.Itoa(cover.Stream), "-c", "copy"}
	if !slices.Contains(copyableCoverCodecs, cover.Codec) {
		arguments = append(arguments, "-c:v:0", "mjpeg", "-q:v", "2")
	}
	return append(arguments, "-disposition:v:0", "attached_pic")
}

type metadataField struct {
	key   string
	value string
}

func buildMetadataArguments(metadata tracks.TrackMetadata) []string {
	fields := []metadataField{
		{key: "title", value: metadata.Title},
		{key: "artist", value: metadata.Artist},
		{key: "album_artist", value: metadata.AlbumArtist},
		{key: "album", value: metadata.Album},
		{key: "genre", value: metadata.Genre},
		{key: "track", value: metadata.Track},
		{key: "disc", value: metadata.Disc},
		{key: "date", value: metadata.Date},
		{key: "composer", value: metadata.Composer},
		{key: "copyright", value: metadata.Copyright},
	}
	arguments := []string{}
	for _, field := range fields {
		value := strings.TrimSpace(tagLineBreaks.Replace(field.value))
		if value != "" {
			arguments = append(arguments, "-metadata", field.key+"="+value)
		}
	}
	return arguments
}
