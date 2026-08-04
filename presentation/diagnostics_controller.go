package presentation

import (
	"context"

	"github.com/UrielJaloto/music-ops/services"
)

type HealthChecker interface {
	Check(ctx context.Context) services.DiagnosticsOutput
}

type DiagnosticsResultPresenter interface {
	Diagnosed(output services.DiagnosticsOutput) int
}

type DiagnosticsController struct {
	checker   HealthChecker
	presenter DiagnosticsResultPresenter
}

func NewDiagnosticsController(checker HealthChecker, presenter DiagnosticsResultPresenter) DiagnosticsController {
	return DiagnosticsController{checker: checker, presenter: presenter}
}

func (controller DiagnosticsController) Execute(ctx context.Context, _ []string) int {
	return controller.presenter.Diagnosed(controller.checker.Check(ctx))
}
