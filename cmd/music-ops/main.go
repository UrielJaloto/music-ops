package main

import (
	"context"
	"io"
	"os"

	"github.com/UrielJaloto/music-ops/infrastructure"
	"github.com/UrielJaloto/music-ops/presentation"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], terminal{input: os.Stdin, output: os.Stdout, errors: os.Stderr}, os.LookupEnv))
}

type terminal struct {
	input  io.Reader
	output io.Writer
	errors io.Writer
}

func run(arguments []string, console terminal, environment func(string) (string, bool)) int {
	errorPresenter := presentation.NewErrorPresenter(console.errors)
	request, err := presentation.ParseCommandLine(arguments)
	if err != nil {
		return errorPresenter.Present(err)
	}
	interrupted, stop := infrastructure.InterruptContext(context.Background(), os.Exit)
	defer stop()
	ctx, cancel := withTestCancellation(interrupted, environment)
	defer cancel()
	paths := infrastructure.ReadEnvironmentPaths(environment)
	store := infrastructure.NewConfigStore(infrastructure.DefaultConfigPath(paths.AppData), environment)
	config, err := store.Load(ctx, request.Overrides)
	if err != nil {
		return errorPresenter.Present(err)
	}
	application := newWiring(wiringSettings{config: config, paths: paths, request: request, console: console, environment: environment, store: store})
	code := application.router().Route(ctx, request)
	application.close(context.WithoutCancel(ctx))
	if err := infrastructure.NewConsoleSession(console.input, console.output).WaitForExit(); err != nil {
		return errorPresenter.Present(err)
	}
	return code
}
