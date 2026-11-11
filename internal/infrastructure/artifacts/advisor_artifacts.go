package artifacts

import (
	"errors"
	"os"

	"github.com/UrielJaloto/music-ops/internal/services/setup"
)

const (
	AdvisorModelName        = "Qwen3.5-9B-Q4_K_M"
	AdvisorServerExecutable = advisorServerName + executableSuffix
	advisorServerName       = "llama-server"
	modelRepositoryBase     = "https://huggingface.co/unsloth/Qwen3.5-9B-GGUF/resolve/3885219b6810b007914f3a7950a8d1b469d598a5/"
)

func BuildAdvisorModelArtifact() setup.ArtifactRequest {
	return setup.ArtifactRequest{
		Name:      "Qwen3.5-9B-Q4_K_M.gguf",
		URL:       modelRepositoryBase + "Qwen3.5-9B-Q4_K_M.gguf",
		SHA256:    "03b74727a860a56338e042c4420bb3f04b2fec5734175f4cb9fa853daf52b7e8",
		SizeBytes: 5680522464,
	}
}

const stagingFilePermission = 0o600

const stagingFolderPermission = 0o750

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
