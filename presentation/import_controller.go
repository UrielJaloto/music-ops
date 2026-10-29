package presentation

import (
	"context"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

const chooseFolderTitle = "Choose the folder to import"

type FolderImporter interface {
	Import(ctx context.Context, input services.ImportInput) (services.ImportOutput, error)
}

type ImportPreviewer interface {
	Preview(ctx context.Context, input services.ImportInput) (services.ImportPreviewOutput, error)
}

type FolderChooser interface {
	ChooseFolder(ctx context.Context, title, start string) (string, error)
}

type RunFinisher interface {
	Finish(ctx context.Context, output services.ImportOutput) error
}

type RunReporter interface {
	Present(ctx context.Context, output services.ImportOutput) (string, error)
}

type ImportResultPresenter interface {
	Imported(output services.ImportOutput, reportPath string)
	ReportFailed(err error)
	Previewed(output services.ImportPreviewOutput)
	FinishFailed(err error)
	Failed(err error) int
}

type ImportDefaults struct {
	Output        string
	Profile       domain.EncodingProfile
	Numbering     domain.NumberingMode
	Layout        domain.Layout
	AlbumTemplate string
}

type ImportServices struct {
	Importer  FolderImporter
	Previewer ImportPreviewer
	Reporter  RunReporter
}

type ImportDesktop struct {
	Chooser  FolderChooser
	Finisher RunFinisher
}

type ImportSettings struct {
	Defaults   ImportDefaults
	OpensFiles bool
}

type ImportController struct {
	services  ImportServices
	desktop   ImportDesktop
	presenter ImportResultPresenter
	settings  ImportSettings
}

func NewImportController(importServices ImportServices, desktop ImportDesktop, presenter ImportResultPresenter, settings ImportSettings) ImportController {
	return ImportController{services: importServices, desktop: desktop, presenter: presenter, settings: settings}
}

func (controller ImportController) Run(ctx context.Context, arguments []string) int {
	input, err := controller.importInput(ctx, arguments)
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

func (controller ImportController) report(ctx context.Context, output services.ImportOutput) string {
	path, err := controller.services.Reporter.Present(ctx, output)
	if err != nil {
		controller.presenter.ReportFailed(err)
	}
	return path
}

func (controller ImportController) Preview(ctx context.Context, arguments []string) int {
	input, err := controller.importInput(ctx, arguments)
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

func (controller ImportController) finish(ctx context.Context, output services.ImportOutput) {
	if !controller.settings.OpensFiles {
		return
	}
	if err := controller.desktop.Finisher.Finish(ctx, output); err != nil {
		controller.presenter.FinishFailed(err)
	}
}

func (controller ImportController) importInput(ctx context.Context, arguments []string) (services.ImportInput, error) {
	folder, err := controller.folder(ctx, arguments)
	if err != nil {
		return services.ImportInput{}, err
	}
	defaults := controller.settings.Defaults
	return services.ImportInput{
		Folder: folder, Output: defaults.Output, Profile: defaults.Profile,
		Numbering: defaults.Numbering, Layout: defaults.Layout, AlbumTemplate: defaults.AlbumTemplate,
	}, nil
}

func (controller ImportController) folder(ctx context.Context, arguments []string) (string, error) {
	if len(arguments) > 0 {
		return arguments[0], nil
	}
	return controller.desktop.Chooser.ChooseFolder(ctx, chooseFolderTitle, "")
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
