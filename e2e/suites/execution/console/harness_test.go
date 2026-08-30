package console

import (
	"archive/tar"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
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
	consoleProcessExitTimeout      = 5 * time.Second
)

//go:embed testdata/console/*.js
var consoleFixtures embed.FS

type consoleContainerConfig struct {
	image    string
	rpcURL   string
	scenario string
}

type consoleContainerEngine interface {
	createContainer(context.Context, consoleContainerConfig) (string, error)
	copyFixtures(context.Context, string) error
	startContainer(context.Context, string) (consoleContainerProcess, error)
	removeContainer(context.Context, string) error
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

func (engine dockerConsoleEngine) createContainer(ctx context.Context, config consoleContainerConfig) (string, error) {
	endpoint, err := consoleContainerEndpoint(config.rpcURL)
	if err != nil {
		return "", fmt.Errorf("create console suite %s container: %w", config.scenario, err)
	}

	arguments := []string{
		"attach",
		"--datadir", consoleContainerDataDir,
		"--jspath", consoleContainerJSPath,
		"--exec", "loadScript('harness.js');loadScript('assertions.js');loadScript('" + config.scenario + ".js')",
	}
	arguments = append(arguments, endpoint)

	created, err := engine.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image:        config.image,
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
		return "", fmt.Errorf("create console suite %s container: %w", config.scenario, err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("create console suite %s container: Docker returned no container ID", config.scenario)
	}
	return created.ID, nil
}

func (engine dockerConsoleEngine) copyFixtures(ctx context.Context, containerID string) error {
	archive, err := consoleFixtureArchive()
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

func (engine dockerConsoleEngine) startContainer(
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

func (engine dockerConsoleEngine) removeContainer(ctx context.Context, containerID string) error {
	if _, err := engine.client.ContainerRemove(ctx, containerID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove Docker container: %w", err)
	}
	return nil
}

type dockerConsoleProcess struct {
	ctx       context.Context
	cancel    context.CancelFunc
	attach    dockerclient.ContainerAttachResult
	waiter    dockerclient.ContainerWaitResult
	closeOnce sync.Once
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

func consoleContainerEndpoint(rpcURL string) (string, error) {
	endpoint, err := url.Parse(rpcURL)
	if err != nil {
		return "", fmt.Errorf("parse console endpoint: %w", err)
	}
	port := endpoint.Port()
	if endpoint.Scheme == "" || endpoint.Hostname() == "" || port == "" {
		return "", errors.New("parse console endpoint: URL must include a scheme, host, and port")
	}

	endpoint.Host = net.JoinHostPort(consoleContainerHost, port)
	return endpoint.String(), nil
}

func consoleFixtureArchive() ([]byte, error) {
	entries, err := consoleFixtures.ReadDir("testdata/console")
	if err != nil {
		return nil, fmt.Errorf("read console fixtures: %w", err)
	}

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	fixtureDirectory := path.Base(consoleContainerJSPath)
	if err := writer.WriteHeader(&tar.Header{
		Name:     fixtureDirectory,
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	}); err != nil {
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		contents, err := consoleFixtures.ReadFile(path.Join("testdata/console", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read console fixture %s: %w", entry.Name(), err)
		}
		if err := writer.WriteHeader(&tar.Header{
			Name:     path.Join(fixtureDirectory, entry.Name()),
			Mode:     0o644,
			Size:     int64(len(contents)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, fmt.Errorf("archive console fixture %s: %w", entry.Name(), err)
		}
		if _, err := writer.Write(contents); err != nil {
			return nil, fmt.Errorf("archive console fixture %s: %w", entry.Name(), err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("archive console fixtures: %w", err)
	}
	return archive.Bytes(), nil
}

func runSuite(ctx context.Context, image, rpcURL, name string) error {
	client, err := dockerapi.New()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = client.Close() }()
	return runSuiteWithEngine(ctx, image, rpcURL, name, dockerConsoleEngine{client: client})
}

func runSuiteWithEngine(
	ctx context.Context,
	image string,
	rpcURL string,
	name string,
	engine consoleContainerEngine,
) (result error) {
	config := consoleContainerConfig{
		image:    image,
		rpcURL:   rpcURL,
		scenario: name,
	}
	containerID, err := engine.createContainer(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), consoleContainerCleanupTimeout)
		defer cancel()
		if err := engine.removeContainer(cleanupCtx, containerID); err != nil {
			result = errors.Join(result, fmt.Errorf("remove console suite %s container: %w", config.scenario, err))
		}
	}()
	if err := engine.copyFixtures(ctx, containerID); err != nil {
		return fmt.Errorf("copy console suite %s fixtures: %w", config.scenario, err)
	}

	process, err := engine.startContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("start console suite %s: %w", config.scenario, err)
	}
	defer process.close()
	return runSuiteProcess(ctx, process, config.scenario)
}

func runSuiteProcess(ctx context.Context, process consoleContainerProcess, name string) error {
	var output bytes.Buffer
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
		outputErr = <-outputDone
	}
	resultOutput := output.Bytes()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("console suite %s: %w\n%s", name, err, resultOutput)
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
	timer := time.NewTimer(consoleProcessExitTimeout)
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
	timer := time.NewTimer(consoleProcessExitTimeout)
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
			fmt.Errorf("console output stream did not close within %s", consoleProcessExitTimeout),
			<-outputDone,
		)
	}
}

func parseSuiteResult(name string, output []byte) error {
	successMarker := []byte(resultPrefix + name)
	failureMarker := []byte(failurePrefix + name)
	failureDetailPrefix := []byte(failurePrefix + name + " ")
	successes := 0
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if bytes.Equal(line, failureMarker) ||
			bytes.HasPrefix(line, failureDetailPrefix) {
			return fmt.Errorf("console suite %s emitted a failure marker", name)
		}
		if bytes.Equal(line, successMarker) {
			successes++
		}
	}
	if successes != 1 {
		return fmt.Errorf("console suite %s emitted %d success markers", name, successes)
	}
	return nil
}
