// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cyyber/qrl-tests/internal/clef"
	"github.com/cyyber/qrl-tests/internal/fixture"
	signercore "github.com/theQRL/go-qrl/signer/core"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	args, cleanup, err := clefArgs(ctx, arguments)
	if err != nil {
		return err
	}
	defer cleanup()

	process, err := clef.Start(ctx, "clef-bin", args, automatedUI(), os.Stderr)
	if err != nil {
		return err
	}
	defer process.Stop()
	if err := process.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func automatedUI() *clef.UI {
	return &clef.UI{
		ApproveTransaction: func(request *signercore.SignTxRequest) bool {
			if request.Transaction.Value.ToInt().Cmp(big.NewInt(fixture.RemoteSignerRejectedTransaction)) == 0 {
				return false
			}
			if request.Transaction.Value.ToInt().Cmp(big.NewInt(fixture.RemoteSignerDelayedTransaction)) == 0 {
				time.Sleep(3 * time.Second)
			}
			return true
		},
		ApproveData: func(request *signercore.SignDataRequest) bool {
			for _, message := range request.Messages {
				if strings.Contains(fmt.Sprint(message.Value), fixture.RemoteSignerRejectedText) {
					return false
				}
			}
			return true
		},
		Input: func(signercore.UserInputRequest) string { return fixture.RemoteSignerPassword },
	}
}

func clefArgs(ctx context.Context, args []string) ([]string, func(), error) {
	dir, err := os.MkdirTemp("", "go-qrl-clef-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	passwordPath := filepath.Join(dir, "password")
	seedPath := filepath.Join(dir, "seed")
	keystorePath := filepath.Join(dir, "keystore")
	if err := os.WriteFile(passwordPath, []byte(fixture.RemoteSignerPassword), 0o600); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.WriteFile(seedPath, []byte(fixture.RemoteSignerSeed), 0o600); err != nil {
		cleanup()
		return nil, nil, err
	}

	importer := exec.CommandContext(ctx, "clef-bin",
		"--suppress-bootwarn",
		"--keystore="+keystorePath,
		"importraw",
		"--password="+passwordPath,
		seedPath,
	)
	importer.Stdout = os.Stderr
	importer.Stderr = os.Stderr
	if err := importer.Run(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("import development account: %w", err)
	}

	configured := make([]string, 0, len(args)+2)
	keystoreSet := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--keystore=") {
			arg = "--keystore=" + keystorePath
			keystoreSet = true
		}
		configured = append(configured, arg)
	}
	if !keystoreSet {
		configured = append(configured, "--keystore="+keystorePath)
	}
	return append(configured, "--stdio-ui"), cleanup, nil
}
