package cli

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
)

type HealthChecker interface {
	Diagnose(ctx context.Context) (diagnostics.DiagnosticsOutput, error)
}

type DiagnosticsResultPresenter interface {
	Diagnosed(output diagnostics.DiagnosticsOutput) int
	Failed(err error) int
}

type DiagnosticsController struct {
	checker   HealthChecker
	presenter DiagnosticsResultPresenter
}

func NewDiagnosticsController(checker HealthChecker, presenter DiagnosticsResultPresenter) DiagnosticsController {
	return DiagnosticsController{checker: checker, presenter: presenter}
}

func (controller DiagnosticsController) Execute(ctx context.Context, _ []string) int {
	output, err := controller.checker.Diagnose(ctx)
	code := controller.presenter.Diagnosed(output)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return code
}
