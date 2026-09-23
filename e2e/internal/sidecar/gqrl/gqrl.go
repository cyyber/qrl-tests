// Package gqrl runs a gqrl console in a sidecar, attached to a devnet
// execution client.
package gqrl

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/e2e/internal/sidecar"
)

const (
	sidecarName = "gqrl console"
	// jsPath is the console's --jspath, where Scripts are copied.
	jsPath = "/tmp/testdata/console"
	// dataDir holds the console's history file.
	dataDir = "/tmp/qrl-tests-console"
)

// Config describes a gqrl console attached to a devnet execution client.
type Config struct {
	// Image provides the gqrl binary.
	Image string
	// EndpointURL is the execution client's RPC URL on the host, such as
	// http://127.0.0.1:8545. In the container, its host is host.docker.internal.
	EndpointURL string
	// Scripts are copied to the console's --jspath by base name.
	Scripts []sidecar.File
	// Run names the scripts to run at startup, in order.
	Run []string
	// Interactive keeps the console reading stdin after Run instead of exiting.
	Interactive bool
}

// Attach starts the console. Close the returned process to remove it.
func Attach(ctx context.Context, config Config) (*sidecar.Process, error) {
	spec, err := consoleSpec(config)
	if err != nil {
		return nil, err
	}
	return sidecar.AttachDocker(ctx, spec)
}

func consoleSpec(config Config) (sidecar.Spec, error) {
	command, err := consoleCommand(config)
	if err != nil {
		return sidecar.Spec{}, err
	}

	scripts, err := sidecar.FilesIn(jsPath, config.Scripts)
	if err != nil {
		return sidecar.Spec{}, err
	}

	return sidecar.Spec{
		Name:       sidecarName,
		Image:      config.Image,
		Entrypoint: []string{"gqrl"},
		Cmd:        command,
		Files:      scripts,
		Stdin:      config.Interactive,
	}, nil
}

func consoleCommand(config Config) ([]string, error) {
	endpoint, err := sidecar.HostURL(config.EndpointURL)
	if err != nil {
		return nil, fmt.Errorf("rewrite console endpoint: %w", err)
	}

	if len(config.Run) == 0 {
		return nil, errors.New("console has no scripts to run")
	}

	command := []string{"attach", "--datadir", dataDir, "--jspath", jsPath}
	if config.Interactive {
		command = append(command, "--preload", strings.Join(config.Run, ","))
	} else {
		loads := make([]string, len(config.Run))
		for index, name := range config.Run {
			loads[index] = "loadScript(" + strconv.Quote(name) + ")"
		}
		command = append(command, "--exec", strings.Join(loads, ";"))
	}

	return append(command, endpoint), nil
}
