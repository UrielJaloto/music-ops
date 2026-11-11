//go:build !windows

package artifacts

import "github.com/UrielJaloto/music-ops/internal/services/setup"

const executableSuffix = ""

func BuildAdvisorRuntimeArtifacts() []setup.ArtifactRequest {
	return []setup.ArtifactRequest{}
}
