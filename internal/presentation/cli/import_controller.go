package cli

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type FolderImporter interface {
	Import(ctx context.Context, input importing.ImportInput) (importing.ImportOutput, error)
}

type ImportPreviewer interface {
	Preview(ctx context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error)
}

type ProfileSource interface {
	SelectProfile() audio.EncodingProfile
}

type RunFinisher interface {
	Finish(ctx context.Context, output importing.ImportOutput) error
}

type RunReporter interface {
	Present(ctx context.Context, output importing.ImportOutput) (string, error)
}

type ImportResultPresenter interface {
	Imported(output importing.ImportOutput, reportPath string)
	ReportFailed(err error)
	Previewed(output importing.ImportPreviewOutput)
	FinishFailed(err error)
	Failed(err error) int
}

type ImportDefaults struct {
	Output        string
	Profile       ProfileSource
	Numbering     albums.NumberingMode
	Layout        filename.Layout
	AlbumTemplate string
	Tags          tracks.TagOverrides
	Genres        genres.Pool
}

type ImportServices struct {
	Importer  FolderImporter
	Previewer ImportPreviewer
	Reporter  RunReporter
}

type ImportSettings struct {
	Defaults   ImportDefaults
	OpensFiles bool
}

type ImportController struct {
	services  ImportServices
	finisher  RunFinisher
	presenter ImportResultPresenter
	settings  ImportSettings
}

func NewImportController(importServices ImportServices, finisher RunFinisher, presenter ImportResultPresenter, settings ImportSettings) ImportController {
	return ImportController{services: importServices, finisher: finisher, presenter: presenter, settings: settings}
}

func (controller ImportController) Run(ctx context.Context, arguments []string) int {
	input, err := controller.readImportInput(arguments)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	output, err := controller.services.Importer.Import(ctx, input)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	controller.presenter.Imported(output, controller.report(ctx, output))
	controller.finish(ctx, output)
	return output.ExitStatus
}

func (controller ImportController) report(ctx context.Context, output importing.ImportOutput) string {
	path, err := controller.services.Reporter.Present(ctx, output)
	if err != nil {
		controller.presenter.ReportFailed(err)
	}
	return path
}

func (controller ImportController) Preview(ctx context.Context, arguments []string) int {
	input, err := controller.readImportInput(arguments)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	output, err := controller.services.Previewer.Preview(ctx, input)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	controller.presenter.Previewed(output)
	return exitSuccess
}

func (controller ImportController) finish(ctx context.Context, output importing.ImportOutput) {
	if !controller.settings.OpensFiles {
		return
	}
	if err := controller.finisher.Finish(ctx, output); err != nil {
		controller.presenter.FinishFailed(err)
	}
}

func (controller ImportController) readImportInput(arguments []string) (importing.ImportInput, error) {
	input, err := buildImportInput(controller.settings.Defaults)
	if err != nil {
		return importing.ImportInput{}, err
	}
	if len(arguments) > 0 {
		input.Folder = arguments[0]
	}
	return input, nil
}

func buildImportInput(defaults ImportDefaults) (importing.ImportInput, error) {
	tags, err := defaults.Tags.CanonicalizeGenre(defaults.Genres)
	if err != nil {
		return importing.ImportInput{}, err
	}
	return importing.ImportInput{
		Output: defaults.Output, Profile: defaults.Profile.SelectProfile(),
		Numbering: defaults.Numbering, Layout: defaults.Layout, AlbumTemplate: defaults.AlbumTemplate, Tags: tags,
	}, nil
}

type RunCommand struct {
	controller ImportController
}

func NewRunCommand(controller ImportController) RunCommand {
	return RunCommand{controller: controller}
}

func (command RunCommand) Execute(ctx context.Context, arguments []string) int {
	return command.controller.Run(ctx, arguments)
}

type PreviewCommand struct {
	controller ImportController
}

func NewPreviewCommand(controller ImportController) PreviewCommand {
	return PreviewCommand{controller: controller}
}

func (command PreviewCommand) Execute(ctx context.Context, arguments []string) int {
	return command.controller.Preview(ctx, arguments)
}
