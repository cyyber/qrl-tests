package kurtosis

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func (*Client) StartServices(ctx context.Context, enclave string, names ...string) error {
	return serviceCommand(ctx, "start", enclave, names)
}

func (*Client) StopServices(ctx context.Context, enclave string, names ...string) error {
	return serviceCommand(ctx, "stop", enclave, names)
}

func serviceCommand(ctx context.Context, action, enclave string, names []string) error {
	arguments := append([]string{"service", action, enclave}, names...)
	output, err := exec.CommandContext(ctx, "kurtosis", arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
