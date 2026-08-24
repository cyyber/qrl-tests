package console

import (
	"archive/tar"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cyyber/qrl-tests/internal/dockerapi"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const (
	resultPrefix  = "CONSOLE_E2E_PASS "
	failurePrefix = "CONSOLE_E2E_FAIL "

	consoleContainerJSPath         = "/tmp/qrl-tests-js"
	consoleContainerDataDir        = "/tmp/qrl-tests-console"
	consoleContainerHost           = "host.docker.internal"
	consoleContainerCleanupTimeout = 30 * time.Second
	watchedSuiteExitTimeout        = 5 * time.Second
)

type consoleScenario struct {
	name        string
	description string
}

var consoleScenarios = []consoleScenario{
	{
		name:        "api",
		description: "validates console and RPC APIs against the live network",
	},
}

//go:embed testdata/console/*.js
var consoleFixtures embed.FS

type consoleContainerSpec struct {
	image    string
	endpoint string
	scenario string
}

type consoleContainerEngine interface {
	create(context.Context, consoleContainerSpec) (string, error)
	copyFixtures(context.Context, string, string) error
	start(context.Context, string) (consoleContainerProcess, error)
	remove(context.Context, string) error
}

type consoleContainerProcess interface {
	readOutput(io.Writer) error
	wait() error
	close()
}

type consoleDockerClient interface {
	ContainerAttach(context.Context, string, dockerclient.ContainerAttachOptions) (dockerclient.ContainerAttachResult, error)
	ContainerCreate(context.Context, dockerclient.ContainerCreateOptions) (dockerclient.ContainerCreateResult, error)
	ContainerRemove(context.Context, string, dockerclient.ContainerRemoveOptions) (dockerclient.ContainerRemoveResult, error)
	ContainerStart(context.Context, string, dockerclient.ContainerStartOptions) (dockerclient.ContainerStartResult, error)
	ContainerWait(context.Context, string, dockerclient.ContainerWaitOptions) dockerclient.ContainerWaitResult
	CopyToContainer(context.Context, string, dockerclient.CopyToContainerOptions) (dockerclient.CopyToContainerResult, error)
}

type dockerConsoleEngine struct {
	client consoleDockerClient
}

type dockerConsoleProcess struct {
	ctx       context.Context
	cancel    context.CancelFunc
	attach    dockerclient.ContainerAttachResult
	waiter    dockerclient.ContainerWaitResult
	closeOnce sync.Once
}

func newDockerConsoleEngine() (*dockerConsoleEngine, io.Closer, error) {
	client, err := dockerapi.New()
	if err != nil {
		return nil, nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &dockerConsoleEngine{client: client}, client, nil
}

func consoleContainerEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse console endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("parse console endpoint: URL must include a scheme and host")
	}

	host := strings.TrimSuffix(parsed.Hostname(), ".")
	if host == "" {
		return "", fmt.Errorf("parse console endpoint: URL must include a host")
	}
	address := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (address == nil || !address.IsLoopback()) {
		return endpoint, nil
	}

	port := parsed.Port()
	parsed.Host = consoleContainerHost
	if port != "" {
		parsed.Host = net.JoinHostPort(consoleContainerHost, port)
	}
	return parsed.String(), nil
}

func (engine dockerConsoleEngine) create(ctx context.Context, spec consoleContainerSpec) (string, error) {
	endpoint, err := consoleContainerEndpoint(spec.endpoint)
	if err != nil {
		return "", fmt.Errorf("create console suite %s container: %w", spec.scenario, err)
	}

	arguments := []string{
		"attach",
		"--datadir", consoleContainerDataDir,
		"--jspath", consoleContainerJSPath,
		"--exec", "loadScript('harness.js');loadScript('" + spec.scenario + ".js')",
	}
	arguments = append(arguments, endpoint)

	created, err := engine.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:        spec.image,
			Entrypoint:   []string{"gqrl"},
			Cmd:          arguments,
			AttachStdout: true,
			AttachStderr: true,
		},
		HostConfig: &containertypes.HostConfig{
			ExtraHosts: []string{consoleContainerHost + ":host-gateway"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create console suite %s container: %w", spec.scenario, err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("create console suite %s container: Docker returned no container ID", spec.scenario)
	}
	return created.ID, nil
}

func (engine dockerConsoleEngine) copyFixtures(ctx context.Context, containerID, jsPath string) error {
	jsPath, err := filepath.Abs(jsPath)
	if err != nil {
		return fmt.Errorf("resolve console fixture directory: %w", err)
	}
	archive, err := consoleFixtureArchive(jsPath)
	if err != nil {
		return err
	}
	if _, err := engine.client.CopyToContainer(ctx, containerID, dockerclient.CopyToContainerOptions{
		DestinationPath: path.Dir(consoleContainerJSPath),
		Content:         bytes.NewReader(archive),
	}); err != nil {
		return fmt.Errorf("copy fixtures into console container: %w", err)
	}
	return nil
}

func consoleFixtureArchive(jsPath string) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	err := filepath.WalkDir(jsPath, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(filePath)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(jsPath, filePath)
		if err != nil {
			return err
		}
		header.Name = path.Join(path.Base(consoleContainerJSPath), filepath.ToSlash(relative))
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}
	return archive.Bytes(), nil
}

func (engine dockerConsoleEngine) start(
	ctx context.Context,
	containerID string,
) (consoleContainerProcess, error) {
	processCtx, cancel := context.WithCancel(ctx)
	attached, err := engine.client.ContainerAttach(processCtx, containerID, dockerclient.ContainerAttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("attach to console container: %w", err)
	}
	waiter := engine.client.ContainerWait(processCtx, containerID, dockerclient.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNextExit,
	})
	if _, err := engine.client.ContainerStart(processCtx, containerID, dockerclient.ContainerStartOptions{}); err != nil {
		cancel()
		attached.Close()
		return nil, fmt.Errorf("start console container: %w", err)
	}
	return &dockerConsoleProcess{
		ctx:    processCtx,
		cancel: cancel,
		attach: attached,
		waiter: waiter,
	}, nil
}

func (process *dockerConsoleProcess) readOutput(destination io.Writer) error {
	_, err := stdcopy.StdCopy(destination, destination, process.attach.Reader)
	return err
}

func (process *dockerConsoleProcess) wait() error {
	select {
	case response := <-process.waiter.Result:
		if response.Error != nil {
			return errors.New(response.Error.Message)
		}
		if response.StatusCode != 0 {
			return fmt.Errorf("exit status %d", response.StatusCode)
		}
		return nil
	case err := <-process.waiter.Error:
		return err
	case <-process.ctx.Done():
		return process.ctx.Err()
	}
}

func (process *dockerConsoleProcess) close() {
	process.closeOnce.Do(func() {
		process.cancel()
		process.attach.Close()
	})
}

func (engine dockerConsoleEngine) remove(ctx context.Context, containerID string) error {
	if _, err := engine.client.ContainerRemove(ctx, containerID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove Docker container: %w", err)
	}
	return nil
}

func removeConsoleContainer(engine consoleContainerEngine, containerID, scenario string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), consoleContainerCleanupTimeout)
	defer cancel()
	if err := engine.remove(cleanupCtx, containerID); err != nil {
		return fmt.Errorf("remove console suite %s container: %w", scenario, err)
	}
	return nil
}

func runSuite(ctx context.Context, image, jsPath, rpcURL, name string) (result error) {
	engine, closer, err := newDockerConsoleEngine()
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, closer.Close())
	}()
	return runSuiteWithEngine(ctx, image, jsPath, rpcURL, name, engine)
}

func runSuiteWithEngine(
	ctx context.Context,
	image string,
	jsPath string,
	rpcURL string,
	name string,
	engine consoleContainerEngine,
) (result error) {
	spec := consoleContainerSpec{
		image:    image,
		endpoint: rpcURL,
		scenario: name,
	}
	containerID, err := engine.create(ctx, spec)
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, removeConsoleContainer(engine, containerID, spec.scenario))
	}()
	if err := engine.copyFixtures(ctx, containerID, jsPath); err != nil {
		return fmt.Errorf("copy console suite %s fixtures: %w", spec.scenario, err)
	}

	process, err := engine.start(ctx, containerID)
	if err != nil {
		return fmt.Errorf("start console suite %s: %w", spec.scenario, err)
	}
	defer process.close()
	return runSuiteProcess(ctx, process, spec.scenario)
}

func runSuiteProcess(ctx context.Context, process consoleContainerProcess, name string) error {
	var output synchronizedBuffer
	outputDone := make(chan error, 1)
	go func() {
		outputDone <- process.readOutput(&output)
	}()
	processDone := make(chan error, 1)
	go func() {
		processDone <- process.wait()
	}()

	var processErr, outputErr error
	select {
	case processErr = <-processDone:
		outputErr = waitForConsoleOutput(ctx, process, outputDone)
	case outputErr = <-outputDone:
		if outputErr != nil {
			process.close()
		} else {
			processErr = waitForConsoleExit(ctx, process, processDone)
		}
	case <-ctx.Done():
		process.close()
		processErr = ctx.Err()
		outputErr = <-outputDone
	}
	resultOutput := output.Bytes()
	if ctx.Err() != nil {
		return fmt.Errorf("console suite %s: %w\n%s", name, ctx.Err(), resultOutput)
	}
	if processErr != nil {
		return fmt.Errorf("run console suite %s: %w\n%s", name, processErr, resultOutput)
	}
	if outputErr != nil {
		return fmt.Errorf("read console suite %s output: %w\n%s", name, outputErr, resultOutput)
	}
	if err := parseSuiteResult(name, resultOutput); err != nil {
		return fmt.Errorf("%w\n%s", err, resultOutput)
	}
	return nil
}

func waitForConsoleExit(
	ctx context.Context,
	process consoleContainerProcess,
	processDone <-chan error,
) error {
	timer := time.NewTimer(watchedSuiteExitTimeout)
	defer timer.Stop()
	select {
	case err := <-processDone:
		return err
	case <-ctx.Done():
		process.close()
		return ctx.Err()
	case <-timer.C:
		process.close()
		return fmt.Errorf("console output stream closed before the container exited")
	}
}

func waitForConsoleOutput(
	ctx context.Context,
	process consoleContainerProcess,
	outputDone <-chan error,
) error {
	timer := time.NewTimer(watchedSuiteExitTimeout)
	defer timer.Stop()
	select {
	case err := <-outputDone:
		return err
	case <-ctx.Done():
		process.close()
		return <-outputDone
	case <-timer.C:
		process.close()
		return errors.Join(
			fmt.Errorf("console output stream did not close within %s", watchedSuiteExitTimeout),
			<-outputDone,
		)
	}
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

func parseSuiteResult(name string, output []byte) error {
	matches, failed := suiteMarkers(name, output)
	if failed {
		return fmt.Errorf("console suite %s emitted a failure marker", name)
	}
	if matches != 1 {
		return fmt.Errorf("console suite %s emitted %d success markers", name, matches)
	}
	return nil
}

func suiteMarkers(name string, output []byte) (successes int, failed bool) {
	successMarker := []byte(resultPrefix + name)
	failureMarker := []byte(failurePrefix + name)
	failureDetailPrefix := []byte(failurePrefix + name + " ")
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if bytes.Equal(line, failureMarker) ||
			bytes.HasPrefix(line, failureDetailPrefix) {
			failed = true
		}
		if bytes.Equal(line, successMarker) {
			successes++
		}
	}
	return successes, failed
}

func prepareWorkspace(destination string) error {
	consoleScripts, err := fs.Sub(consoleFixtures, "testdata/console")
	if err != nil {
		return fmt.Errorf("open console fixtures: %w", err)
	}
	if err := os.CopyFS(destination, consoleScripts); err != nil {
		return fmt.Errorf("copy console fixtures: %w", err)
	}
	return nil
}
