package console

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"
)

type blockingConsole struct {
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

type immediateConsole struct {
	evaluated chan struct{}
}

func (console *immediateConsole) Evaluate(string) { close(console.evaluated) }

func (*immediateConsole) Stop(bool) error { return nil }

func (console *blockingConsole) Evaluate(string) {
	close(console.started)
	<-console.stopped
}

func (console *blockingConsole) Stop(bool) error {
	console.once.Do(func() { close(console.stopped) })
	return nil
}

func TestParseSuiteResult(t *testing.T) {
	valid := []byte(
		`CONSOLE_E2E_PASS api`,
	)
	if err := parseSuiteResult("api", valid); err != nil {
		t.Fatal(err)
	}

	if err := parseSuiteResult("api", []byte(`CONSOLE_E2E_FAIL api`)); err == nil {
		t.Fatal("failed suite was accepted")
	}
}

func TestSuiteFixtures(t *testing.T) {
	names := []string{"harness"}
	for _, scenario := range consoleScenarios {
		names = append(names, scenario.name)
	}
	for _, name := range names {
		if _, err := fs.Stat(consoleFixtures, "testdata/console/"+name+".js"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEvaluateWatchedSuiteStopsBlockedEvaluation(t *testing.T) {
	console := &blockingConsole{started: make(chan struct{}), stopped: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	clientClosed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- evaluateWatchedSuite(ctx, console, func() { close(clientClosed) }, &synchronizedBuffer{}, "events")
	}()

	select {
	case <-console.started:
	case <-time.After(time.Second):
		t.Fatal("evaluation did not start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked evaluation did not stop")
	}
	select {
	case <-clientClosed:
	default:
		t.Fatal("RPC client was not closed")
	}
}

func TestEvaluateWatchedSuiteWaitsForAsynchronousResult(t *testing.T) {
	console := &immediateConsole{evaluated: make(chan struct{})}
	output := &synchronizedBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- evaluateWatchedSuite(t.Context(), console, func() {}, output, "events")
	}()

	select {
	case <-console.evaluated:
	case <-time.After(time.Second):
		t.Fatal("evaluation did not return")
	}
	select {
	case err := <-done:
		t.Fatalf("suite completed before its asynchronous result: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	_, _ = output.Write([]byte("CONSOLE_E2E_PASS events\n"))

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("suite did not observe its asynchronous result")
	}
}
