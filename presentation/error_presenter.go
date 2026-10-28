package presentation

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/UrielJaloto/music-ops/services"
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
	{target: services.ErrFolderMissing, message: "the folder is missing or does not exist", code: exitFailure},
	{target: services.ErrOutputInsideInput, message: "the output folder must not be inside the input folder", code: exitFailure},
	{target: services.ErrSettingUnknown, message: "unknown setting; run: music-ops config show", code: exitFailure},
	{target: services.ErrSettingInvalid, message: "invalid setting value", code: exitFailure},
	{target: services.ErrSettingKeyMissing, message: "a setting key is required", code: exitFailure},
	{target: services.ErrLibraryBusy, message: "another music-ops run is using this library", code: exitFailure},
	{target: services.ErrFFmpegMissing, message: "ffmpeg was not found; install it with: winget install -e --id Gyan.FFmpeg", code: exitFailure},
	{target: services.ErrLocalAdvisorNotInstalled, message: "the local advisor is not installed; run: music-ops advisor install", code: exitFailure},
	{target: services.ErrLastFMKeyInvalid, message: "the Last.fm API key is invalid; run: music-ops config set lastfm.api_key <key>", code: exitFailure},
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
	if _, writeErr := fmt.Fprintf(presenter.output, "error: %s\n", message); writeErr != nil {
		return exitFailure
	}
	return code
}

func describeError(err error) (string, int) {
	for _, candidate := range errorMessages {
		if errors.Is(err, candidate.target) {
			return candidate.message, candidate.code
		}
	}
	return genericMessage + ": " + err.Error(), exitFailure
}
