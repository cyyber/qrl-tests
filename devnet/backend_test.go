// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package devnet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBackend(t *testing.T) {
	backend, err := ParseBackend("")
	require.NoError(t, err)
	require.Equal(t, BackendDocker, backend)

	backend, err = ParseBackend("kubernetes")
	require.NoError(t, err)
	require.Equal(t, BackendKubernetes, backend)

	_, err = ParseBackend("unknown")
	require.Error(t, err)
}

func TestKubernetesImagesUseRegistry(t *testing.T) {
	images := Images{
		Execution: "registry.example/go-qrl:test",
		Clef:      "registry.example/go-qrl-clef:test",
		Consensus: "registry.example/qrysm-beacon:test",
		Validator: "registry.example/qrysm-validator:test",
		Genesis:   "registry.example/qrl-genesis:test",
	}
	require.NoError(t, images.validate(BackendKubernetes))

	images.Execution = DefaultExecutionImage
	require.ErrorContains(t, images.validate(BackendKubernetes), "not available to Kubernetes")
	require.NoError(t, images.validate(BackendDocker))
}
