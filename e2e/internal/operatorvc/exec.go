package operatorvc

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	dockerclient "github.com/moby/moby/client"
)

// VoluntaryExit signs and submits exits for every imported key through the
// validator binary's accounts command. That path dials the beacon itself and
// does not use the keymanager HTTP API.
func (validator *Validator) VoluntaryExit(ctx context.Context) error {
	if validator == nil {
		return fmt.Errorf("operator validator is nil")
	}
	_, err := validator.Exec(ctx,
		"accounts", "voluntary-exit",
		"--accept-terms-of-use",
		"--wallet-dir="+walletDir,
		"--wallet-password-file="+passwordPath,
		"--account-password-file="+passwordPath,
		"--beacon-rpc-provider="+validator.beaconGRPC,
		"--exit-all",
		"--force-exit",
	)
	return err
}

// Exec runs /validator with the given arguments inside the sidecar.
func (validator *Validator) Exec(ctx context.Context, args ...string) (string, error) {
	if validator == nil || validator.exec == nil {
		return "", fmt.Errorf("operator validator exec client is not configured")
	}
	command := append([]string{"/validator"}, args...)
	created, err := validator.exec.ExecCreate(ctx, validator.id, dockerclient.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          command,
	})
	if err != nil {
		return "", fmt.Errorf("create validator exec %s: %w", strings.Join(args, " "), err)
	}
	attached, err := validator.exec.ExecAttach(ctx, created.ID, dockerclient.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("attach validator exec %s: %w", strings.Join(args, " "), err)
	}
	defer attached.Close()

	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, attached.Reader); err != nil {
		return output.String(), fmt.Errorf("read validator exec %s: %w", strings.Join(args, " "), err)
	}
	inspected, err := validator.exec.ExecInspect(ctx, created.ID, dockerclient.ExecInspectOptions{})
	if err != nil {
		return output.String(), fmt.Errorf("inspect validator exec %s: %w", strings.Join(args, " "), err)
	}
	if inspected.ExitCode != 0 {
		return output.String(), fmt.Errorf("validator %s: exit %d: %s", strings.Join(args, " "), inspected.ExitCode, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}
