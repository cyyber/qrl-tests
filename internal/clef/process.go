// Package clef owns the programmable UI and stdio RPC lifecycle used by the
// development Clef image.
package clef

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/theQRL/go-qrl/rpc"
)

type Process struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	input  io.WriteCloser
	output io.ReadCloser
	client *rpc.Client
	once   sync.Once
}

func Start(ctx context.Context, path string, args []string, ui any, stderr io.Writer) (*Process, error) {
	processCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, path, args...)
	command.Stderr = stderr

	input, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open Clef stdin: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		_ = input.Close()
		return nil, fmt.Errorf("open Clef stdout: %w", err)
	}
	client, err := rpc.DialIO(processCtx, output, input)
	if err != nil {
		cancel()
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("create Clef UI client: %w", err)
	}
	if err := client.RegisterName("ui", ui); err != nil {
		cancel()
		client.Close()
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("register Clef UI service: %w", err)
	}
	if err := command.Start(); err != nil {
		cancel()
		client.Close()
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("start Clef: %w", err)
	}
	process := &Process{
		cancel: cancel,
		done:   make(chan struct{}),
		input:  input,
		output: output,
		client: client,
	}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	return process, nil
}

func (process *Process) Wait() error {
	<-process.done
	return process.err
}

func (process *Process) Stop() {
	process.once.Do(func() {
		process.cancel()
		process.client.Close()
		_ = process.input.Close()
		_ = process.output.Close()
		<-process.done
	})
}
