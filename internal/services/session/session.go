package session

import (
	"context"
	"errors"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/UrielJaloto/music-ops/internal/services/watching"
)

const chooseFolderTitle = "Choose the folder to import"

type Workspace interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
}

type Importer interface {
	Import(ctx context.Context, input importing.ImportInput) (importing.ImportOutput, error)
	Preview(ctx context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error)
}

type Watcher interface {
	Watch(ctx context.Context, input watching.WatchInput) error
}

type Diagnoser interface {
	Check(ctx context.Context) diagnostics.DiagnosticsOutput
}

type FolderChooser interface {
	ChooseFolder(ctx context.Context, title, start string) (string, error)
}

type SessionService struct {
	workspace Workspace
	importer  Importer
	watcher   Watcher
	diagnoser Diagnoser
	chooser   FolderChooser
}

func NewSessionService(workspace Workspace, importer Importer, watcher Watcher, diagnoser Diagnoser, chooser FolderChooser) SessionService {
	return SessionService{workspace: workspace, importer: importer, watcher: watcher, diagnoser: diagnoser, chooser: chooser}
}

func (service SessionService) Import(ctx context.Context, input importing.ImportInput) (importing.ImportOutput, error) {
	return runInWorkspace(ctx, service.workspace, func() (importing.ImportOutput, error) {
		chosen, err := service.ensureFolder(ctx, input)
		if err != nil {
			return importing.ImportOutput{}, err
		}
		return service.importer.Import(ctx, chosen)
	})
}

func (service SessionService) Preview(ctx context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error) {
	return runInWorkspace(ctx, service.workspace, func() (importing.ImportPreviewOutput, error) {
		chosen, err := service.ensureFolder(ctx, input)
		if err != nil {
			return importing.ImportPreviewOutput{}, err
		}
		return service.importer.Preview(ctx, chosen)
	})
}

func (service SessionService) Watch(ctx context.Context, input watching.WatchInput) error {
	_, err := runInWorkspace(ctx, service.workspace, func() (struct{}, error) {
		return struct{}{}, service.watcher.Watch(ctx, input)
	})
	return err
}

func (service SessionService) Diagnose(ctx context.Context) (diagnostics.DiagnosticsOutput, error) {
	output := service.diagnoser.Check(ctx)
	return output, service.workspace.Close(context.WithoutCancel(ctx))
}

func (service SessionService) ensureFolder(ctx context.Context, input importing.ImportInput) (importing.ImportInput, error) {
	if strings.TrimSpace(input.Folder) != "" {
		return input, nil
	}
	folder, err := service.chooser.ChooseFolder(ctx, chooseFolderTitle, "")
	if err != nil {
		return importing.ImportInput{}, err
	}
	input.Folder = folder
	return input, nil
}

func runInWorkspace[T any](ctx context.Context, workspace Workspace, work func() (T, error)) (T, error) {
	if err := workspace.Open(ctx); err != nil {
		var none T
		return none, err
	}
	result, err := work()
	return result, errors.Join(err, workspace.Close(context.WithoutCancel(ctx)))
}
