package presentation

import (
	"context"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	usageAdvisor      = "usage: music-ops advisor install"
	installSubcommand = "install"
)

type AdvisorInstaller interface {
	InstallAdvisor(ctx context.Context, input services.InstallAdvisorInput) (services.InstalledAdvisorOutput, error)
}

type SetupResultPresenter interface {
	Installed(output services.InstalledAdvisorOutput) int
	Usage(text string) int
	Failed(err error) int
}

type AdvisorArtifacts struct {
	Folder               string
	Runtime              []services.ArtifactRequest
	Model                services.ArtifactRequest
	ServerExecutableName string
}

type SetupController struct {
	installer AdvisorInstaller
	presenter SetupResultPresenter
	artifacts AdvisorArtifacts
}

func NewSetupController(installer AdvisorInstaller, presenter SetupResultPresenter, artifacts AdvisorArtifacts) SetupController {
	return SetupController{installer: installer, presenter: presenter, artifacts: artifacts}
}

func (controller SetupController) Execute(ctx context.Context, arguments []string) int {
	if len(arguments) != 1 || arguments[0] != installSubcommand {
		return controller.presenter.Usage(usageAdvisor)
	}
	output, err := controller.installer.InstallAdvisor(ctx, services.InstallAdvisorInput{
		Folder: controller.artifacts.Folder, Runtime: controller.artifacts.Runtime,
		Model: controller.artifacts.Model, ServerExecutableName: controller.artifacts.ServerExecutableName,
	})
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Installed(output)
}
