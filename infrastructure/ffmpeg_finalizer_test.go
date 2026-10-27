package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func TestFFmpegFinalizer_ArgumentsGolden(t *testing.T) {
	metadata := domain.TrackMetadata{Title: "Song\nPart 2", Artist: "Main", Genre: "Rock", Track: "3/12"}
	tagArguments := []string{"-map_metadata", "-1", "-metadata", "title=Song Part 2", "-metadata", "artist=Main", "-metadata", "genre=Rock", "-metadata", "track=3/12"}
	cases := []struct {
		name    string
		request services.FinalizeRequest
		middle  []string
	}{
		{name: "WithoutCover", request: services.FinalizeRequest{Staged: "s.m4a", Cover: domain.CoverArt{Stream: -1}, CoverSource: "src.flac", Metadata: metadata}, middle: []string{"-map", "0:a:0", "-c", "copy"}},
		{name: "WithoutCoverSource", request: services.FinalizeRequest{Staged: "s.m4a", Cover: domain.CoverArt{Stream: 1, Codec: "png"}, Metadata: metadata}, middle: []string{"-map", "0:a:0", "-c", "copy"}},
		{name: "PNGCoverCopied", request: services.FinalizeRequest{Staged: "s.m4a", CoverSource: "src.flac", Cover: domain.CoverArt{Stream: 1, Codec: "png"}, Metadata: metadata}, middle: []string{"-i", "src.flac", "-map", "0:a:0", "-map", "1:1", "-c", "copy", "-disposition:v:0", "attached_pic"}},
		{name: "BMPCoverReencoded", request: services.FinalizeRequest{Staged: "s.m4a", CoverSource: "src.flac", Cover: domain.CoverArt{Stream: 2, Codec: "bmp"}, Metadata: metadata}, middle: []string{"-i", "src.flac", "-map", "0:a:0", "-map", "1:2", "-c", "copy", "-c:v:0", "mjpeg", "-q:v", "2", "-disposition:v:0", "attached_pic"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &outputWritingRunner{}
			destination := filepath.Join(t.TempDir(), "final.m4a")
			request := testCase.request
			request.Destination = destination

			err := NewFFmpegFinalizer(runner, "ffmpeg").Finalize(context.Background(), request)

			expected := append([]string{"-hide_banner", "-nostdin", "-y", "-i", "s.m4a"}, testCase.middle...)
			expected = append(append(expected, tagArguments...), "-f", "mp4", "-movflags", "+faststart", destination+".tmp")
			if diff := cmp.Diff(expected, runner.arguments); diff != "" || err != nil || !isFile(destination) {
				t.Fatalf("arguments (-want +got):\n%s err = %v", diff, err)
			}
		})
	}
}

func TestMetadataArguments_AllFieldsInOrder(t *testing.T) {
	metadata := domain.TrackMetadata{Title: "T", Artist: "A", AlbumArtist: "AA", Album: "Al", Genre: "G", Track: "1/2", Disc: "1/1", Date: "2024", Composer: "C", Copyright: "Co"}

	arguments := metadataArguments(metadata)

	expected := []string{"-metadata", "title=T", "-metadata", "artist=A", "-metadata", "album_artist=AA", "-metadata", "album=Al", "-metadata", "genre=G", "-metadata", "track=1/2", "-metadata", "disc=1/1", "-metadata", "date=2024", "-metadata", "composer=C", "-metadata", "copyright=Co"}
	if diff := cmp.Diff(expected, arguments); diff != "" {
		t.Fatalf("arguments (-want +got):\n%s", diff)
	}
}

func TestMetadataArguments_EmptyAndBlankOmitted(t *testing.T) {
	arguments := metadataArguments(domain.TrackMetadata{Title: "T", Artist: " ", Album: "\r\n"})

	if diff := cmp.Diff([]string{"-metadata", "title=T"}, arguments); diff != "" {
		t.Fatalf("arguments (-want +got):\n%s", diff)
	}
}
