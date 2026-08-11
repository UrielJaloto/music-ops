package cli

import (
	"context"

	"github.com/UrielJaloto/music-ops/internal/services/settings"
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
	Show(ctx context.Context) (settings.SettingsOutput, error)
	Get(ctx context.Context, key string) (settings.SettingValueOutput, error)
	Set(ctx context.Context, input settings.SetSettingInput) error
	Path() settings.SettingsLocationOutput
}

type SettingsResultPresenter interface {
	Shown(output settings.SettingsOutput) int
	Got(output settings.SettingValueOutput) int
	Changed(input settings.SetSettingInput) int
	Located(output settings.SettingsLocationOutput) int
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
		"show": controller.showAll,
		"get":  controller.showValue,
		"set":  controller.setValue,
		"path": controller.showPath,
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

func (controller SettingsController) showAll(ctx context.Context, _ []string) int {
	output, err := controller.settings.Show(ctx)
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Shown(output)
}

func (controller SettingsController) showValue(ctx context.Context, arguments []string) int {
	if len(arguments) != getArguments {
		return controller.presenter.Usage(usageConfigGet)
	}
	output, err := controller.settings.Get(ctx, arguments[keyArgument])
	if err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Got(output)
}

func (controller SettingsController) setValue(ctx context.Context, arguments []string) int {
	if len(arguments) != setArguments {
		return controller.presenter.Usage(usageConfigSet)
	}
	input := settings.SetSettingInput{Key: arguments[keyArgument], Value: arguments[valueArgument]}
	if err := controller.settings.Set(ctx, input); err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Changed(input)
}

func (controller SettingsController) showPath(context.Context, []string) int {
	return controller.presenter.Located(controller.settings.Path())
}
