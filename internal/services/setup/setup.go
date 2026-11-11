package setup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/services/discovery"
)

var ErrSetupArtifactMissing = errors.New("advisor runtime or model artifact is missing")

const (
	SettingLlamaServer  = "advisor.llama_server"
	SettingModelFile    = "advisor.model_file"
	runtimeFolderName   = "llama.cpp"
	modelsFolderName    = "models"
	downloadsFolderName = "downloads"
)

type ArtifactRequest struct {
	Name      string
	URL       string
	SHA256    string
	SizeBytes int64
}

type ArtifactDownloader interface {
	Download(ctx context.Context, artifact ArtifactRequest, destination string) error
}

type ArchiveExtractor interface {
	Extract(ctx context.Context, archive, destination string) error
}

type SetupSettings interface {
	Save(ctx context.Context, key, value string) error
}

type SetupProgress interface {
	ArtifactStarted(name string, sizeBytes int64)
	ArtifactProgressed(name string, doneBytes int64)
	ArtifactFinished(name string)
}

type InstallAdvisorInput struct {
	Folder               string
	Runtime              []ArtifactRequest
	Model                ArtifactRequest
	ServerExecutableName string
}

type InstalledAdvisorOutput struct {
	ServerPath string
	ModelPath  string
}

type SetupService struct {
	downloader ArtifactDownloader
	extractor  ArchiveExtractor
	settings   SetupSettings
	progress   SetupProgress
}

func NewSetupService(downloader ArtifactDownloader, extractor ArchiveExtractor, settings SetupSettings, progress SetupProgress) SetupService {
	return SetupService{downloader: downloader, extractor: extractor, settings: settings, progress: progress}
}

func (service SetupService) InstallAdvisor(ctx context.Context, input InstallAdvisorInput) (InstalledAdvisorOutput, error) {
	if err := validateSetupInput(input); err != nil {
		return InstalledAdvisorOutput{}, err
	}
	folder := filepath.Clean(input.Folder)
	runtimeFolder := filepath.Join(folder, runtimeFolderName)
	for _, artifact := range input.Runtime {
		if err := service.installRuntime(ctx, artifact, folder, runtimeFolder); err != nil {
			return InstalledAdvisorOutput{}, err
		}
	}
	modelPath := filepath.Join(folder, modelsFolderName, input.Model.Name)
	if err := service.download(ctx, input.Model, modelPath); err != nil {
		return InstalledAdvisorOutput{}, err
	}
	installed := InstalledAdvisorOutput{ServerPath: filepath.Join(runtimeFolder, input.ServerExecutableName), ModelPath: modelPath}
	if err := service.saveSettings(ctx, installed); err != nil {
		return InstalledAdvisorOutput{}, err
	}
	return installed, nil
}

func validateSetupInput(input InstallAdvisorInput) error {
	if strings.TrimSpace(input.Folder) == "" {
		return discovery.ErrFolderMissing
	}
	if len(input.Runtime) == 0 || strings.TrimSpace(input.Model.URL) == "" || strings.TrimSpace(input.ServerExecutableName) == "" {
		return ErrSetupArtifactMissing
	}
	return nil
}

func (service SetupService) installRuntime(ctx context.Context, artifact ArtifactRequest, folder, runtimeFolder string) error {
	archive := filepath.Join(folder, downloadsFolderName, artifact.Name)
	if err := service.download(ctx, artifact, archive); err != nil {
		return err
	}
	if err := service.extractor.Extract(ctx, archive, runtimeFolder); err != nil {
		return fmt.Errorf("extracting %s: %w", artifact.Name, err)
	}
	return nil
}

func (service SetupService) download(ctx context.Context, artifact ArtifactRequest, destination string) error {
	service.progress.ArtifactStarted(artifact.Name, artifact.SizeBytes)
	if err := service.downloader.Download(ctx, artifact, destination); err != nil {
		return fmt.Errorf("downloading %s: %w", artifact.Name, err)
	}
	service.progress.ArtifactFinished(artifact.Name)
	return nil
}

func (service SetupService) saveSettings(ctx context.Context, installed InstalledAdvisorOutput) error {
	if err := service.settings.Save(ctx, SettingLlamaServer, installed.ServerPath); err != nil {
		return fmt.Errorf("saving %s: %w", SettingLlamaServer, err)
	}
	if err := service.settings.Save(ctx, SettingModelFile, installed.ModelPath); err != nil {
		return fmt.Errorf("saving %s: %w", SettingModelFile, err)
	}
	return nil
}
