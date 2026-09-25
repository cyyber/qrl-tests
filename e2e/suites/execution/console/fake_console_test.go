package console

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/gqrl"
)

// runFake runs scenario on a fake console in the background and checks that
// the run closes the console once.
func runFake(
	t *testing.T,
	ctx context.Context,
	scenario consoleScenario,
	config fakeConsoleConfig,
) (*fakeConsole, <-chan error) {
	process := newFakeConsole(ctx, config)
	done := make(chan error, 1)
	go func() {
		err := runScenarioWith(ctx, scenario, fakeScripts,
			func(context.Context, gqrl.Config) (consoleProcess, error) {
				return process, nil
			})
		if process.closes != 1 {
			t.Errorf("console closed %d times, want 1", process.closes)
		}
		done <- err
	}()
	return process, done
}

// fakeConsoleConfig scripts a fake console.
type fakeConsoleConfig struct {
	// output is written first, then Output returns readErr if set.
	output  string
	readErr error
	// exitOutput is written once exit is sent.
	exitOutput string
	// running keeps the container up until exit is sent or it is detached.
	// Otherwise it exits right away.
	running bool
	waitErr error
	// exitBlocked holds CloseInput until its ctx ends.
	exitBlocked    bool
	exitRequestErr error
	closeErr       error
	// Output and Wait block until their gate is closed.
	readGate, waitGate <-chan struct{}
	// endGate, if set, ends Output when it closes rather than when the
	// container exits.
	endGate <-chan struct{}
}

// closedGate returns a gate that is already open.
func closedGate() <-chan struct{} {
	gate := make(chan struct{})
	close(gate)
	return gate
}

// fakeConsole is a consoleProcess that follows its config.
type fakeConsole struct {
	ctx          context.Context
	cancel       context.CancelFunc
	config       fakeConsoleConfig
	detached     chan struct{}
	exit         chan struct{}
	exitOutput   chan struct{}
	detachOnce   sync.Once
	exitRequests atomic.Int32
	closes       int
}

func newFakeConsole(ctx context.Context, config fakeConsoleConfig) *fakeConsole {
	processCtx, cancel := context.WithCancel(ctx)
	return &fakeConsole{
		ctx:        processCtx,
		cancel:     cancel,
		config:     config,
		detached:   make(chan struct{}),
		exit:       make(chan struct{}),
		exitOutput: make(chan struct{}),
	}
}

func (process *fakeConsole) Output(destination io.Writer) error {
	if process.config.readGate != nil {
		<-process.config.readGate
	}
	if _, err := io.WriteString(destination, process.config.output); err != nil {
		return err
	}
	if process.config.readErr != nil {
		return process.config.readErr
	}
	if process.config.exitOutput != "" {
		select {
		case <-process.exit:
			if _, err := io.WriteString(destination, process.config.exitOutput); err != nil {
				return err
			}
			close(process.exitOutput)
		case <-process.detached:
			return nil
		}
	}
	if process.config.endGate != nil {
		<-process.config.endGate
		return nil
	}
	<-process.detached
	return nil
}

func (process *fakeConsole) Wait() error {
	if process.config.waitGate != nil {
		<-process.config.waitGate
	}
	// The output streams close when the container exits.
	defer process.Detach()
	if !process.config.running {
		return process.config.waitErr
	}
	// With exitOutput, the container exits once it has printed it.
	exited := process.exit
	if process.config.exitOutput != "" {
		exited = process.exitOutput
	}
	select {
	case <-exited:
		return process.config.waitErr
	case <-process.ctx.Done():
		return fmt.Errorf("wait for gqrl console: %w", context.Cause(process.ctx))
	}
}

func (process *fakeConsole) CloseInput(ctx context.Context, _ string) error {
	process.exitRequests.Add(1)
	if process.config.exitBlocked {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	if process.config.exitRequestErr != nil {
		return process.config.exitRequestErr
	}
	close(process.exit)
	return nil
}

func (process *fakeConsole) Detach() {
	process.detachOnce.Do(func() {
		process.cancel()
		close(process.detached)
	})
}

func (process *fakeConsole) Close() error {
	process.closes++
	process.Detach()
	return process.config.closeErr
}
