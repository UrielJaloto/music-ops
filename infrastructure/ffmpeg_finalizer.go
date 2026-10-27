package infrastructure

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
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

func (finalizer FFmpegFinalizer) Finalize(ctx context.Context, request services.FinalizeRequest) error {
	return runToPartial(ctx, finalizer.runner, finalizer.executable, finalizeArguments(request), request.Destination)
}

func finalizeArguments(request services.FinalizeRequest) []string {
	hasCover := request.CoverSource != "" && request.Cover.Stream >= 0
	arguments := []string{"-hide_banner", "-nostdin", "-y", "-i", processPath(request.Staged)}
	if hasCover {
		arguments = append(arguments, "-i", processPath(request.CoverSource))
	}
	arguments = append(arguments, "-map", "0:a:0")
	if hasCover {
		arguments = append(arguments, coverArguments(request.Cover)...)
	} else {
		arguments = append(arguments, "-c", "copy")
	}
	arguments = append(arguments, "-map_metadata", "-1")
	arguments = append(arguments, metadataArguments(request.Metadata)...)
	return append(arguments, "-f", "mp4", "-movflags", "+faststart", processPath(request.Destination+partialSuffix))
}

func coverArguments(cover domain.CoverArt) []string {
	arguments := []string{"-map", "1:" + strconv.Itoa(cover.Stream), "-c", "copy"}
	if !slices.Contains(copyableCoverCodecs, cover.Codec) {
		arguments = append(arguments, "-c:v:0", "mjpeg", "-q:v", "2")
	}
	return append(arguments, "-disposition:v:0", "attached_pic")
}

func metadataArguments(metadata domain.TrackMetadata) []string {
	fields := []struct{ key, value string }{
		{"title", metadata.Title}, {"artist", metadata.Artist}, {"album_artist", metadata.AlbumArtist},
		{"album", metadata.Album}, {"genre", metadata.Genre}, {"track", metadata.Track}, {"disc", metadata.Disc},
		{"date", metadata.Date}, {"composer", metadata.Composer}, {"copyright", metadata.Copyright},
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
