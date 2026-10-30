package infrastructure

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWatchInterrupts(t *testing.T) {
	cases := []struct {
		name         string
		signals      int
		expectedExit int
	}{
		{name: "FirstSignalCancels", signals: 1, expectedExit: -1},
		{name: "SecondSignalForcesExit", signals: 2, expectedExit: 130},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			signals := make(chan os.Signal, 2)
			exited := make(chan int, 1)
			for range testCase.signals {
				signals <- os.Interrupt
			}
			if testCase.signals == 1 {
				close(signals)
			}

			watchInterrupts(ctx, cancel, signals, func(code int) { exited <- code })

			exitCode := -1
			select {
			case exitCode = <-exited:
			case <-time.After(time.Second):
			}
			if ctx.Err() == nil || exitCode != testCase.expectedExit {
				t.Fatalf("ctx = %v exit = %d", ctx.Err(), exitCode)
			}
		})
	}
}

func TestWatchInterrupts_ParentDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	watchInterrupts(ctx, cancel, make(chan os.Signal), func(int) { t.Fatal("unexpected exit") })
}

func TestInterruptContext_Stop(t *testing.T) {
	ctx, stop := InterruptContext(context.Background(), func(int) {})

	stop()

	if ctx.Err() == nil {
		t.Fatal("context not cancelled by stop")
	}
}
