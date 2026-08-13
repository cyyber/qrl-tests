package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cyyber/qrl-tests/internal/clef"
	"github.com/theQRL/go-qrl/accounts/keystore"
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
	args, password, cleanup, err := clefArgs(arguments)
	if err != nil {
		return err
	}
	defer cleanup()

	process, err := clef.Start(ctx, "clef-bin", args, &clef.UI{Password: password}, os.Stderr)
	if err != nil {
		return err
	}
	defer process.Stop()
	if err := process.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func clefArgs(args []string) ([]string, string, func(), error) {
	dir, err := os.MkdirTemp("", "qrl-tests-clef-")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("generate Clef account password: %w", err)
	}
	password := hex.EncodeToString(passwordBytes)
	keystorePath := filepath.Join(dir, "keystore")
	store := keystore.NewKeyStore(
		keystorePath,
		keystore.LightArgon2idT,
		keystore.LightArgon2idM,
		keystore.LightArgon2idP,
	)
	if _, err := store.NewAccount(password); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("create Clef development account: %w", err)
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
	return append(configured, "--stdio-ui"), password, cleanup, nil
}
