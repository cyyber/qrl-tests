package console

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingConsole struct {
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

type completedConsole struct{}

func (completedConsole) Evaluate(string) {}

func (completedConsole) Stop(bool) error { return nil }

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

func TestEvaluateWatchedSuiteRejectsMissingResult(t *testing.T) {
	err := evaluateWatchedSuite(
		t.Context(),
		completedConsole{},
		func() {},
		&synchronizedBuffer{},
		"events",
	)
	if err == nil || !strings.Contains(err.Error(), "emitted 0 success markers") {
		t.Fatalf("got %v, want missing result error", err)
	}
}
