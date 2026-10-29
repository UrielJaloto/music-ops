package presentation

import (
	"context"

	"github.com/UrielJaloto/music-ops/services"
)

const (
	usageConfig    = "usage: music-ops config show|get <key>|set <key> <value>|path"
	usageConfigGet = "usage: music-ops config get <key>"
	usageConfigSet = "usage: music-ops config set <key> <value>"
	getArguments   = 2
	setArguments   = 3
	keyArgument    = 1
	valueArgument  = 2
)

type SettingsManager interface {
	Show(ctx context.Context) (services.SettingsOutput, error)
	Get(ctx context.Context, key string) (services.SettingValueOutput, error)
	Set(ctx context.Context, input services.SetSettingInput) error
	Path() services.SettingsLocationOutput
}

type SettingsResultPresenter interface {
	Shown(output services.SettingsOutput) int
	Got(output services.SettingValueOutput) int
	Changed(input services.SetSettingInput) int
	Located(output services.SettingsLocationOutput) int
	Usage(text string) int
	Failed(err error) int
}

type SettingsController struct {
	settings  SettingsManager
	presenter SettingsResultPresenter
}

func NewSettingsController(settings SettingsManager, presenter SettingsResultPresenter) SettingsController {
	return SettingsController{settings: settings, presenter: presenter}
}

func (controller SettingsController) Execute(ctx context.Context, arguments []string) int {
	subcommands := map[string]func(context.Context, []string) int{
		"show": controller.show,
		"get":  controller.get,
		"set":  controller.set,
		"path": controller.path,
	}
	if len(arguments) == 0 {
		return controller.presenter.Usage(usageConfig)
	}
	subcommand, isKnown := subcommands[arguments[0]]
	if !isKnown {
		return controller.presenter.Usage(usageConfig)
	}
	return subcommand(ctx, arguments)
}

func (controller SettingsController) show(ctx context.Context, _ []string) int {
	output, err := controller.settings.Show(ctx)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Shown(output)
}

func (controller SettingsController) get(ctx context.Context, arguments []string) int {
	if len(arguments) != getArguments {
		return controller.presenter.Usage(usageConfigGet)
	}
	output, err := controller.settings.Get(ctx, arguments[keyArgument])
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Got(output)
}

func (controller SettingsController) set(ctx context.Context, arguments []string) int {
	if len(arguments) != setArguments {
		return controller.presenter.Usage(usageConfigSet)
	}
	input := services.SetSettingInput{Key: arguments[keyArgument], Value: arguments[valueArgument]}
	if err := controller.settings.Set(ctx, input); err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Changed(input)
}

func (controller SettingsController) path(context.Context, []string) int {
	return controller.presenter.Located(controller.settings.Path())
}
