// Copyright 2026 The qrl-tests Authors
// This file is part of qrl-tests.

package devnet

import (
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"
)

func renderCustomParameters(payload []byte, address string, images Images) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}
	shape, err := decodeParameterShape(&document)
	if err != nil {
		return "", err
	}
	if len(shape.Participants) == 0 || shape.Participants[0].ExecutionImage != executionImagePlaceholder {
		return "", fmt.Errorf(
			"first participant el_image must be %q",
			executionImagePlaceholder,
		)
	}
	if _, ok := shape.Network.PrefundedAccounts[walletAddressPlaceholder]; !ok {
		return "", fmt.Errorf(
			"network_params.prefunded_accounts must contain %q",
			walletAddressPlaceholder,
		)
	}

	replaceParameterTokens(&document, map[string]string{
		executionImagePlaceholder: images.Execution,
		clefImagePlaceholder:      images.Clef,
		consensusImagePlaceholder: images.Consensus,
		validatorImagePlaceholder: images.Validator,
		genesisImagePlaceholder:   images.Genesis,
		walletAddressPlaceholder:  address,
	})
	rendered, err := yaml.Marshal(&document)
	if err != nil {
		return "", fmt.Errorf("encode rendered parameters: %w", err)
	}
	return string(rendered), nil
}

func decodeParameterShape(document *yaml.Node) (parameterShape, error) {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return parameterShape{}, errors.New("parameters file must contain one YAML mapping")
	}
	var shape parameterShape
	if err := document.Decode(&shape); err != nil {
		return parameterShape{}, errors.New("parameters file must contain one YAML mapping")
	}
	return shape, nil
}

func replaceParameterTokens(node *yaml.Node, replacements map[string]string) {
	if node.Kind == yaml.ScalarNode {
		if replacement, ok := replacements[node.Value]; ok {
			node.Value = replacement
		}
	}
	for _, child := range node.Content {
		replaceParameterTokens(child, replacements)
	}
}
