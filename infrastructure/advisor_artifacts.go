package infrastructure

import "github.com/UrielJaloto/music-ops/services"

const (
	AdvisorServerExecutable = "llama-server.exe"
	AdvisorModelName        = "Qwen3.5-9B-Q4_K_M"
	llamaReleaseBase        = "https://github.com/ggml-org/llama.cpp/releases/download/b11606/"
	modelRepositoryBase     = "https://huggingface.co/unsloth/Qwen3.5-9B-GGUF/resolve/3885219b6810b007914f3a7950a8d1b469d598a5/"
)

func AdvisorRuntimeArtifacts() []services.ArtifactRequest {
	return []services.ArtifactRequest{
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

func AdvisorModelArtifact() services.ArtifactRequest {
	return services.ArtifactRequest{
		Name:      "Qwen3.5-9B-Q4_K_M.gguf",
		URL:       modelRepositoryBase + "Qwen3.5-9B-Q4_K_M.gguf",
		SHA256:    "03b74727a860a56338e042c4420bb3f04b2fec5734175f4cb9fa853daf52b7e8",
		SizeBytes: 5680522464,
	}
}
