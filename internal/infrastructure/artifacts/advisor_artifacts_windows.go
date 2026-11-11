//go:build windows

package artifacts

import "github.com/UrielJaloto/music-ops/internal/services/setup"

const (
	executableSuffix = ".exe"
	llamaReleaseBase = "https://github.com/ggml-org/llama.cpp/releases/download/b11606/"
)

func BuildAdvisorRuntimeArtifacts() []setup.ArtifactRequest {
	return []setup.ArtifactRequest{
		{
			Name:      "llama-b11606-bin-win-cuda-13.4-x64.zip",
			URL:       llamaReleaseBase + "llama-b11606-bin-win-cuda-13.4-x64.zip",
			SHA256:    "93ba7c16d1ad8343248d540bb4c06e7e18f7d0601e762d6952bad13628295be8",
			SizeBytes: 153479124,
		},
		{
			Name:      "cudart-llama-bin-win-cuda-13.4-x64.zip",
			URL:       llamaReleaseBase + "cudart-llama-bin-win-cuda-13.4-x64.zip",
			SHA256:    "738f8c251ac22b70c3ae6f83a10cf222725df0395246a2cf58f32bdb85fbe668",
			SizeBytes: 423535356,
		},
	}
}
