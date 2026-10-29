package presentation

import (
	"context"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

const usageWatch = "usage: music-ops watch [inbox] (or set watch.inbox)"

type InboxWatcher interface {
	Watch(ctx context.Context, input services.WatchInput) error
}

type WatchResultPresenter interface {
	Stopped() int
	Usage(text string) int
	Failed(err error) int
}

type WatchDefaults struct {
	Inbox       string
	QuietPeriod time.Duration
	Import      ImportDefaults
}

type WatchController struct {
	watcher   InboxWatcher
	presenter WatchResultPresenter
	defaults  WatchDefaults
}

func NewWatchController(watcher InboxWatcher, presenter WatchResultPresenter, defaults WatchDefaults) WatchController {
	return WatchController{watcher: watcher, presenter: presenter, defaults: defaults}
}

func (controller WatchController) Execute(ctx context.Context, arguments []string) int {
	inbox := controller.defaults.Inbox
	if len(arguments) > 0 {
		inbox = arguments[0]
	}
	if inbox == "" {
		return controller.presenter.Usage(usageWatch)
	}
	if err := controller.watcher.Watch(ctx, controller.watchInput(inbox)); err != nil {
		return controller.presenter.Failed(err)
	}
	return controller.presenter.Stopped()
}

func (controller WatchController) watchInput(inbox string) services.WatchInput {
	defaults := controller.defaults.Import
	return services.WatchInput{
		Inbox:       inbox,
		QuietPeriod: controller.defaults.QuietPeriod,
		Import: services.ImportInput{
			Output: defaults.Output, Profile: defaults.Profile, Numbering: defaults.Numbering,
			Layout: defaults.Layout, AlbumTemplate: defaults.AlbumTemplate,
		},
	}
}
