package transcoding

import (
	"context"
	"sync"
)

type Tools struct {
	FFmpeg       string
	FFprobe      string
	Capabilities FFmpegCapabilities
}

type ToolResolver interface {
	ResolveTools(ctx context.Context) (Tools, error)
}

type ToolchainSettings struct {
	FFmpeg   string
	FFprobe  string
	LookPath func(name string) (string, error)
}

type toolchainState struct {
	once  sync.Once
	tools Tools
	err   error
}

type Toolchain struct {
	settings ToolchainSettings
	runner   ProcessExecutor
	state    *toolchainState
}

func NewToolchain(settings ToolchainSettings, runner ProcessExecutor) Toolchain {
	return Toolchain{settings: settings, runner: runner, state: &toolchainState{}}
}

func (toolchain Toolchain) ResolveTools(ctx context.Context) (Tools, error) {
	toolchain.state.once.Do(func() {
		toolchain.state.tools, toolchain.state.err = toolchain.resolve(ctx)
	})
	return toolchain.state.tools, toolchain.state.err
}

func (toolchain Toolchain) resolve(ctx context.Context) (Tools, error) {
	ffmpeg, err := LocateFFmpeg(toolchain.settings.FFmpeg, toolchain.settings.LookPath)
	if err != nil {
		return Tools{}, err
	}
	ffprobe, err := LocateFFprobe(toolchain.settings.FFprobe, toolchain.settings.LookPath)
	if err != nil {
		return Tools{}, err
	}
	capabilities, err := DetectFFmpegCapabilities(ctx, toolchain.runner, ffmpeg)
	if err != nil {
		return Tools{}, err
	}
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe, Capabilities: capabilities}, nil
}
