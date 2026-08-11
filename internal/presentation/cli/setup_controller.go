package cli

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/setup"
)

const (
	usageAdvisor      = "usage: music-ops advisor install"
	installSubcommand = "install"
)

type AdvisorInstaller interface {
	InstallAdvisor(ctx context.Context, input setup.InstallAdvisorInput) (setup.InstalledAdvisorOutput, error)
}

type SetupResultPresenter interface {
	Installed(output setup.InstalledAdvisorOutput) int
	Usage(text string) int
	Failed(err error) int
}

type AdvisorArtifacts struct {
	Folder               string
	Runtime              []setup.ArtifactRequest
	Model                setup.ArtifactRequest
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
	output, err := controller.installer.InstallAdvisor(ctx, setup.InstallAdvisorInput{
		Folder: controller.artifacts.Folder, Runtime: controller.artifacts.Runtime,
		Model: controller.artifacts.Model, ServerExecutableName: controller.artifacts.ServerExecutableName,
	})
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Installed(output)
}
