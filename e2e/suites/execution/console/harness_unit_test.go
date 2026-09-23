package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
	"github.com/cyyber/qrl-tests/e2e/internal/sidecar/gqrl"
	"github.com/stretchr/testify/require"
)

var (
	apiScenario = consoleScenario{
		image:       "image",
		endpointURL: "http://127.0.0.1:8545",
		scenario:    "api",
	}
	eventsScenario = consoleScenario{
		image:       "image",
		endpointURL: "ws://127.0.0.1:8546",
		scenario:    "events",
		interactive: true,
	}
	fakeScripts = []sidecar.File{{Name: "harness.js", Body: []byte("// harness")}}
)

func TestConsoleConfig(t *testing.T) {
	require.Equal(t, gqrl.Config{
		Image:       "image",
		EndpointURL: "http://127.0.0.1:8545",
		Scripts:     fakeScripts,
		Run:         []string{"harness.js", "assertions.js", "api.js"},
	}, consoleConfig(apiScenario, fakeScripts))
	require.Equal(t, gqrl.Config{
		Image:       "image",
		EndpointURL: "ws://127.0.0.1:8546",
		Scripts:     fakeScripts,
		Run:         []string{"harness.js", "assertions.js", "events.js"},
		Interactive: true,
	}, consoleConfig(eventsScenario, fakeScripts))
}

func TestConsoleScripts(t *testing.T) {
	parameters := []byte(`{"chainID":"0x539"}`)
	scripts, err := consoleScripts(parameters)
	require.NoError(t, err)
	for _, script := range scripts {
		require.EqualValues(t, 0o444, script.Mode, script.Name)
	}
	contents := scriptContents(scripts)
	for _, name := range []string{"harness.js", "assertions.js", "api.js"} {
		require.Contains(t, contents, name)
	}
	require.Equal(t, `var PARAMS = {"chainID":"0x539"};`+"\n", contents[".params.js"])

	scripts, err = consoleScripts(nil)
	require.NoError(t, err)
	require.NotContains(t, scriptContents(scripts), ".params.js")
}

func scriptContents(scripts []sidecar.File) map[string]string {
	contents := make(map[string]string, len(scripts))
	for _, script := range scripts {
		contents[script.Name] = string(script.Body)
	}
	return contents
}

func TestParseSuiteResult(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		output     string
		wantDetail string
	}{
		{name: "pass", output: "CONSOLE_E2E_PASS api"},
		{name: "failure", output: "CONSOLE_E2E_FAIL api", wantDetail: "emitted a failure marker"},
		{name: "GoError", output: "GoError: helper failure", wantDetail: "failed with GoError"},
		{
			name:       "failure after pass",
			output:     "CONSOLE_E2E_PASS api\nCONSOLE_E2E_FAIL api unexpected callback",
			wantDetail: "emitted a failure marker",
		},
		{name: "wrong scenario", output: "CONSOLE_E2E_PASS events", wantDetail: "emitted 0 success markers"},
		{name: "invalid suffix", output: "CONSOLE_E2E_PASS api extra", wantDetail: "emitted 0 success markers"},
		{name: "duplicate pass", output: "CONSOLE_E2E_PASS api\nCONSOLE_E2E_PASS api", wantDetail: "emitted 2 success markers"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := parseSuiteResult("api", []byte(testCase.output))
			if testCase.wantDetail == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantDetail)
		})
	}
}

func TestOutputRecorder(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		chunks       []string
		unterminated bool
	}{
		{name: "split marker", chunks: []string{"noise\nCONSOLE_E2E_PA", "SS events\n"}},
		{name: "failure marker", chunks: []string{"CONSOLE_E2E_FAIL events\n"}},
		{name: "GoError", chunks: []string{"GoError: helper failure\n"}},
		{name: "later markers", chunks: []string{"CONSOLE_E2E_PASS events\nCONSOLE_E2E_FAIL events\n"}},
		{name: "unterminated marker", chunks: []string{"CONSOLE_E2E_PASS events"}, unterminated: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			events := make(chan processEvent, 2)
			output := newOutputRecorder("events", events, true)
			for _, chunk := range testCase.chunks {
				_, err := io.WriteString(output, chunk)
				require.NoError(t, err)
			}
			reportedEarly := len(events) > 0
			result := output.complete(nil)

			require.Equal(t, !testCase.unterminated, reportedEarly)
			require.Len(t, events, 1)
			require.Equal(t, eventSignalDetected, (<-events).kind)
			require.Equal(t, strings.Join(testCase.chunks, ""), string(result.output))
		})
	}
}

func TestOutputRecorderSnapshot(t *testing.T) {
	output := newOutputRecorder("api", nil, false)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_, _ = io.WriteString(output, "line\n")
		}
	}()
	// A run that is cut short takes a snapshot while the reader still writes.
	for range 100 {
		output.snapshot()
	}
	<-done
	require.Equal(t, strings.Repeat("line\n", 100), string(output.snapshot()))
}

func TestFinishProcess(t *testing.T) {
	cause := errors.New("suite cancelled")
	exitErr := errors.New("exit request failed")
	for _, testCase := range []struct {
		name    string
		result  processResult
		cause   error
		wantErr error
	}{
		{
			name:    "wait detached by cancellation",
			result:  processResult{containerWaitCompleted: true, containerWaitErr: context.Canceled},
			cause:   cause,
			wantErr: cause,
		},
		{
			name:    "wait ended by the cause",
			result:  processResult{containerWaitCompleted: true, containerWaitErr: fmt.Errorf("wait: %w", cause)},
			cause:   cause,
			wantErr: cause,
		},
		{
			name:   "wait detached by force close",
			result: processResult{containerWaitCompleted: true, containerWaitErr: context.Canceled, forcedClose: true},
		},
		{
			name:    "exit request ended by the cause",
			result:  processResult{forcedClose: true, exitRequestErr: cause},
			cause:   cause,
			wantErr: cause,
		},
		{
			name:   "exit request failed after clean exit",
			result: processResult{containerWaitCompleted: true, exitRequestErr: exitErr},
		},
		{
			name:    "exit request failed and force closed",
			result:  processResult{containerWaitCompleted: true, forcedClose: true, exitRequestErr: exitErr},
			wantErr: exitErr,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.result.output = &outputResult{output: []byte(passPrefix + "events\n")}
			err := finishProcess("events", testCase.result, testCase.cause)
			if testCase.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, testCase.wantErr)
			require.Equal(t, 1, strings.Count(err.Error(), testCase.wantErr.Error()))
			require.NotContains(t, err.Error(), "run console suite events")
		})
	}
}

func TestFinishLateCancellation(t *testing.T) {
	cause := errors.New("suite cancelled")
	cancelled, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	for _, testCase := range []struct {
		name       string
		supervisor processSupervisor
	}{
		{name: "run", supervisor: processSupervisor{ctx: cancelled}},
		{name: "shutdown", supervisor: processSupervisor{ctx: t.Context(), shutdownCtx: cancelled}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			supervisor := testCase.supervisor
			supervisor.name = "api"
			supervisor.result = processResult{
				output:                 &outputResult{output: []byte(passPrefix + "api\n")},
				containerWaitCompleted: true,
			}
			require.ErrorIs(t, supervisor.finish(nil), cause)
		})
	}
}

func TestRespondToEvents(t *testing.T) {
	exitErr := errors.New("exit request failed")
	for _, testCase := range []struct {
		name          string
		result        processResult
		exitRequested bool
		wantForced    bool
	}{
		{
			name:       "exit request failed while running",
			result:     processResult{exitRequestErr: exitErr},
			wantForced: true,
		},
		{
			name:   "exit request failed after exit",
			result: processResult{containerWaitCompleted: true, exitRequestErr: exitErr},
		},
		{
			name:       "output ended before exit request",
			result:     processResult{output: &outputResult{}},
			wantForced: true,
		},
		{
			name:          "output ended after exit request",
			result:        processResult{output: &outputResult{}},
			exitRequested: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			supervisor := &processSupervisor{
				ctx:           t.Context(),
				process:       newFakeConsole(t.Context(), fakeConsoleConfig{}),
				interactive:   true,
				exitRequested: testCase.exitRequested,
				result:        testCase.result,
			}
			defer supervisor.cancelShutdownDeadline()
			supervisor.respondToEvents(false)
			require.Equal(t, testCase.wantForced, supervisor.result.forcedClose)
		})
	}
}

func TestRunStartFailure(t *testing.T) {
	startErr := errors.New("start failed")
	err := runScenarioWith(t.Context(), apiScenario, fakeScripts,
		func(context.Context, gqrl.Config) (consoleProcess, error) {
			return nil, startErr
		})
	require.ErrorIs(t, err, startErr)
	require.ErrorContains(t, err, "start console suite api")
}

func TestRun(t *testing.T) {
	outputErr := errors.New("output failed")
	closeErr := errors.New("close failed")
	exitErr := errors.New("exit request failed")
	processErr := errors.New("process failed")
	for _, testCase := range []struct {
		name       string
		scenario   consoleScenario
		console    fakeConsoleConfig
		wantErr    error
		wantDetail string
		wantExits  int32
	}{
		{name: "non-interactive pass", scenario: apiScenario, console: fakeConsoleConfig{output: passPrefix + "api\n"}},
		{
			name:     "non-interactive output error",
			scenario: apiScenario,
			console:  fakeConsoleConfig{readErr: outputErr, running: true},
			wantErr:  outputErr,
		},
		{
			name:     "non-interactive close error",
			scenario: apiScenario,
			console:  fakeConsoleConfig{output: passPrefix + "api\n", closeErr: closeErr},
			wantErr:  closeErr,
		},
		{
			name:       "non-interactive failure and close error",
			scenario:   apiScenario,
			console:    fakeConsoleConfig{output: failPrefix + "api helper failure\n", closeErr: closeErr},
			wantErr:    closeErr,
			wantDetail: "emitted a failure marker",
		},
		{
			name:      "interactive pass",
			scenario:  eventsScenario,
			console:   fakeConsoleConfig{output: passPrefix + "events\n", running: true},
			wantExits: 1,
		},
		{
			name:     "interactive failure after success",
			scenario: eventsScenario,
			console: fakeConsoleConfig{
				output:     passPrefix + "events\n",
				exitOutput: failPrefix + "events helper failure\n",
				running:    true,
			},
			wantDetail: "emitted a failure marker",
			wantExits:  1,
		},
		{
			name:       "interactive exit request error",
			scenario:   eventsScenario,
			console:    fakeConsoleConfig{output: passPrefix + "events\n", exitRequestErr: exitErr, running: true},
			wantErr:    exitErr,
			wantDetail: "stop console suite events",
			wantExits:  1,
		},
		{
			name:      "interactive process error after pass",
			scenario:  eventsScenario,
			console:   fakeConsoleConfig{output: passPrefix + "events\n", waitErr: processErr, running: true},
			wantErr:   processErr,
			wantExits: 1,
		},
		{
			name:       "interactive early exit",
			scenario:   eventsScenario,
			console:    fakeConsoleConfig{output: "console exited early\n"},
			wantDetail: "emitted 0 success markers",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			process, done := runFake(t, t.Context(), testCase.scenario, testCase.console)
			err := <-done
			if testCase.wantErr == nil && testCase.wantDetail == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "console process did not shut down")
			}
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			if testCase.wantDetail != "" {
				require.ErrorContains(t, err, testCase.wantDetail)
			}
			require.Equal(t, testCase.wantExits, process.exitRequests.Load())
		})
	}
}

func TestRunNonInteractiveJoinsErrors(t *testing.T) {
	processErr := errors.New("process failed")
	outputErr := errors.New("output failed")
	for _, testCase := range []struct {
		name         string
		processFirst bool
	}{
		{name: "process first", processFirst: true},
		{name: "output first"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				readGate := make(chan struct{})
				waitGate := make(chan struct{})
				_, done := runFake(t, t.Context(), apiScenario, fakeConsoleConfig{
					output:   passPrefix + "api\n",
					readErr:  outputErr,
					waitErr:  processErr,
					readGate: readGate,
					waitGate: waitGate,
				})
				first, second := readGate, waitGate
				if testCase.processFirst {
					first, second = waitGate, readGate
				}
				close(first)
				synctest.Wait()
				close(second)

				err := <-done
				require.ErrorIs(t, err, processErr)
				require.ErrorIs(t, err, outputErr)
				require.Equal(t, 1, strings.Count(err.Error(), passPrefix+"api"))
			})
		})
	}
}

func TestRunInteractiveProcessAlreadyExited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		readGate := make(chan struct{})
		endGate := make(chan struct{})
		process, done := runFake(t, t.Context(), eventsScenario, fakeConsoleConfig{
			output:   passPrefix + "events\n",
			readGate: readGate,
			endGate:  endGate,
		})
		// The container exits, then its marker arrives before its output ends.
		synctest.Wait()
		close(readGate)
		synctest.Wait()
		close(endGate)

		require.NoError(t, <-done)
		require.Zero(t, process.exitRequests.Load())
	})
}

func TestRunInteractiveOutputEndsFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		process, done := runFake(t, t.Context(), eventsScenario, fakeConsoleConfig{
			output:  "console stopped writing\n",
			endGate: closedGate(),
			running: true,
		})

		require.ErrorContains(t, <-done, "emitted 0 success markers")
		require.Zero(t, time.Since(started))
		require.Zero(t, process.exitRequests.Load())
	})
}

func TestRunCancellationWithBlockedExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancelErr := errors.New("cancel blocked exit")
		process, done := runFake(t, ctx, eventsScenario, fakeConsoleConfig{
			output:      passPrefix + "events\n",
			exitBlocked: true,
			running:     true,
		})
		synctest.Wait()
		cancel(cancelErr)

		err := <-done
		require.ErrorIs(t, err, cancelErr)
		require.Equal(t, 1, strings.Count(err.Error(), cancelErr.Error()))
		require.EqualValues(t, 1, process.exitRequests.Load())
	})
}

func TestRunCancellationWithBlockedOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancelErr := errors.New("cancel blocked output")
		readGate := make(chan struct{})
		defer close(readGate)
		_, done := runFake(t, ctx, apiScenario, fakeConsoleConfig{readGate: readGate, running: true})
		synctest.Wait()
		cancel(cancelErr)

		err := <-done
		require.ErrorIs(t, err, cancelErr)
		require.Equal(t, 1, strings.Count(err.Error(), cancelErr.Error()))
	})
}

func TestRunShutdownTimeout(t *testing.T) {
	t.Run("blocked exit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			process, done := runFake(t, t.Context(), eventsScenario, fakeConsoleConfig{
				output:      passPrefix + "events\n",
				exitBlocked: true,
				running:     true,
			})

			err := <-done
			require.ErrorContains(t, err, "console process did not shut down within 5s")
			// The error shows what the console printed before it hung.
			require.ErrorContains(t, err, passPrefix+"events")
			require.Equal(t, processExitTimeout, time.Since(started))
			require.EqualValues(t, 1, process.exitRequests.Load())
		})
	})

	t.Run("blocked output", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			readGate := make(chan struct{})
			defer close(readGate)
			started := time.Now()
			_, done := runFake(t, t.Context(), apiScenario, fakeConsoleConfig{
				output:   passPrefix + "api\n",
				readGate: readGate,
			})

			require.ErrorContains(t, <-done, "console process did not shut down within 5s")
			require.Equal(t, processExitTimeout, time.Since(started))
		})
	})

	t.Run("container keeps running", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			_, done := runFake(t, t.Context(), apiScenario, fakeConsoleConfig{
				output:  passPrefix + "api\n",
				endGate: closedGate(),
				running: true,
			})

			require.ErrorContains(t, <-done, "console process did not shut down within 5s")
			require.Equal(t, processExitTimeout, time.Since(started))
		})
	})
}
