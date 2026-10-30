package infrastructure

import (
	"context"
	"os"
	"os/signal"
)

const forcedExitCode = 130

func InterruptContext(parent context.Context, exit func(code int)) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	go watchInterrupts(ctx, cancel, signals, exit)
	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}

func watchInterrupts(ctx context.Context, cancel context.CancelFunc, signals <-chan os.Signal, exit func(code int)) {
	select {
	case <-signals:
		cancel()
	case <-ctx.Done():
		return
	}
	if _, isOpen := <-signals; isOpen {
		exit(forcedExitCode)
	}
}
