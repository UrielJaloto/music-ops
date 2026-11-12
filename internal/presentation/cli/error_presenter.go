package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/UrielJaloto/music-ops/internal/services/settings"
)

const (
	exitCancelled  = 130
	genericMessage = "music-ops failed"
)

type errorMessage struct {
	target  error
	message string
	code    int
}

var errorMessages = []errorMessage{
	{target: context.Canceled, message: "cancelled", code: exitCancelled},
	{target: importing.ErrFolderChoiceCancelled, message: "no folder selected", code: exitSuccess},
	{target: discovery.ErrFolderMissing, message: "the folder is missing or does not exist", code: exitFailure},
	{target: discovery.ErrOutputInsideInput, message: "the output folder must not be inside the input folder", code: exitFailure},
	{target: settings.ErrSettingUnknown, message: "unknown setting; run: music-ops config show", code: exitFailure},
	{target: settings.ErrSettingInvalid, message: "invalid setting value", code: exitFailure},
	{target: settings.ErrSettingKeyMissing, message: "a setting key is required", code: exitFailure},
	{target: importing.ErrRunInProgress, message: "another music-ops run is in progress", code: exitFailure},
	{target: diagnostics.ErrFFmpegMissing, message: "ffmpeg was not found; install it with: winget install -e --id Gyan.FFmpeg", code: exitFailure},
	{target: diagnostics.ErrLocalAdvisorNotInstalled, message: "the local advisor is not installed; run: music-ops advisor install", code: exitFailure},
	{target: diagnostics.ErrLastFMKeyInvalid, message: "the Last.fm API key is invalid; run: music-ops config set lastfm.api_key <key>", code: exitFailure},
	{target: tracks.ErrTagUnknown, message: "unknown tag; use album, genre or year", code: exitFailure},
	{target: tracks.ErrTagValueMissing, message: "a tag value is empty", code: exitFailure},
	{target: tracks.ErrYearInvalid, message: "the year must have four digits", code: exitFailure},
	{target: genres.ErrGenreFormatInvalid, message: `the genre must be "Genre" or "Genre; Subgenre"`, code: exitFailure},
	{target: genres.ErrGenreNotInPool, code: exitFailure},
	{target: genres.ErrSubgenreNotInGenre, code: exitFailure},
	{target: genres.ErrGenreDefinitionInvalid, code: exitFailure},
	{target: genres.ErrGenrePoolEmpty, code: exitFailure},
	{target: ErrCommandLineInvalid, message: "invalid command line; run: music-ops help", code: exitFailure},
}

type ErrorPresenter struct {
	output io.Writer
}

func NewErrorPresenter(output io.Writer) ErrorPresenter {
	return ErrorPresenter{output: output}
}

func (presenter ErrorPresenter) Present(err error) int {
	message, code := describeError(err)
	if code != exitSuccess {
		message = "error: " + message
	}
	if _, writeErr := fmt.Fprintln(presenter.output, message); writeErr != nil {
		return exitFailure
	}
	return code
}

func describeError(err error) (string, int) {
	for _, candidate := range errorMessages {
		if !errors.Is(err, candidate.target) {
			continue
		}
		if candidate.message == "" {
			return err.Error(), candidate.code
		}
		return candidate.message, candidate.code
	}
	return genericMessage + ": " + err.Error(), exitFailure
}
