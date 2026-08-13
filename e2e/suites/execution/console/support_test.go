package console

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/consolefixture"
	endtoendlive "github.com/cyyber/qrl-tests/e2e/internal/live"
	qrlconsole "github.com/theQRL/go-qrl/console"
	"github.com/theQRL/go-qrl/rpc"
)

const resultPrefix = "CONSOLE_E2E_PASS "
const failurePrefix = "CONSOLE_E2E_FAIL "

type consoleScenario struct {
	name        string
	description string
	webSocket   bool
}

var consoleScenarios = []consoleScenario{
	{
		name:        "api",
		description: "validates console and RPC APIs against the live network",
	},
	{
		name:        "contract",
		description: "deploys a contract and validates VM64 ABI, receipts, events, and filters",
	},
	{
		name:        "topics",
		description: "formats and decodes indexed VM64 scalar topics",
	},
	{
		name:        "constructor",
		description: "deploys a contract through the embedded web3 contract factory",
		webSocket:   true,
	},
	{
		name:        "events",
		description: "formats and submits a contract transaction and watches indexed events over WebSocket",
		webSocket:   true,
	},
}

//go:embed testdata/console/*.js
var consoleFixtures embed.FS

func runSuite(ctx context.Context, gqrlPath, jsPath, rpcURL, name string) error {
	expression := "loadScript('harness.js');loadScript('" + name + ".js')"
	command := exec.CommandContext(
		ctx,
		gqrlPath,
		"attach",
		"--jspath",
		jsPath,
		"--exec",
		expression,
		rpcURL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run console suite %s: %w\n%s", name, err, output)
	}
	if err := parseSuiteResult(name, output); err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

type synchronizedBuffer struct {
	lock sync.Mutex
	data bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()
	return buffer.data.Write(data)
}

func (buffer *synchronizedBuffer) Bytes() []byte {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()
	return bytes.Clone(buffer.data.Bytes())
}

type watchedConsole interface {
	Evaluate(string)
	Stop(bool) error
}

func runWatchedSuite(ctx context.Context, webSocketURL, jsPath, name string) error {
	client, err := rpc.DialContext(ctx, webSocketURL)
	if err != nil {
		return fmt.Errorf("connect console suite %s: %w", name, err)
	}
	var output synchronizedBuffer
	console, err := qrlconsole.New(qrlconsole.Config{
		DataDir: filepath.Join(jsPath, "console-data"),
		DocRoot: jsPath,
		Client:  client,
		Printer: &output,
	})
	if err != nil {
		client.Close()
		return fmt.Errorf("create console suite %s: %w", name, err)
	}
	return evaluateWatchedSuite(ctx, console, client.Close, &output, name)
}

func evaluateWatchedSuite(
	ctx context.Context,
	console watchedConsole,
	closeClient func(),
	output *synchronizedBuffer,
	name string,
) error {
	evaluationDone := make(chan struct{})
	go func() {
		defer close(evaluationDone)
		console.Evaluate("loadScript('harness.js');loadScript('" + name + ".js')")
	}()
	defer func() {
		closeClient()
		_ = console.Stop(false)
		<-evaluationDone
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result := output.Bytes()
		if bytes.Contains(result, []byte(resultPrefix+name)) {
			return parseSuiteResult(name, result)
		}
		if bytes.Contains(result, []byte(failurePrefix+name)) ||
			bytes.Contains(result, []byte("GoError:")) {
			return fmt.Errorf("console suite %s failed\n%s", name, result)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("console suite %s: %w\n%s", name, ctx.Err(), result)
		case <-ticker.C:
		}
	}
}

func parseSuiteResult(name string, output []byte) error {
	marker := []byte(resultPrefix + name)
	var matches int
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		if bytes.Equal(bytes.TrimSpace(line), marker) {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("console suite %s emitted %d success markers", name, matches)
	}
	return nil
}

func prepareWorkspace(ctx context.Context, destination string, session *endtoendlive.Node) error {
	consoleScripts, err := fs.Sub(consoleFixtures, "testdata/console")
	if err != nil {
		return fmt.Errorf("open console fixtures: %w", err)
	}
	if err := os.CopyFS(destination, consoleScripts); err != nil {
		return fmt.Errorf("copy console fixtures: %w", err)
	}

	bytecode, err := consolefixture.Bytecode()
	if err != nil {
		return fmt.Errorf("decode console contract bytecode: %w", err)
	}

	params, err := deploymentParameters(ctx, session, consolefixture.ABI, bytecode)
	if err != nil {
		return err
	}
	script := append([]byte("var PARAMS = "), params...)
	script = append(script, ';', '\n')
	if err := os.WriteFile(filepath.Join(destination, ".params.js"), script, 0o600); err != nil {
		return fmt.Errorf("write console parameters: %w", err)
	}
	return nil
}
