// Package jsonfile reads and writes JSON documents.
package jsonfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Read reads and decodes a JSON document from path. The label names the
// document in errors.
func Read[T any](path, label string) (T, error) {
	var zero T
	payload, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read %s %q: %w", label, path, err)
	}

	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, fmt.Errorf("decode %s %q: %w", label, path, err)
	}
	return value, nil
}

// Write encodes value as indented JSON and writes it to path, creating the
// parent directory when needed. The label names the document in errors.
func Write(path string, value any, label string) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", label, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}
