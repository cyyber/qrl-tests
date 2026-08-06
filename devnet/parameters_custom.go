package devnet

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

func customParameters(payload []byte, address string) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}
	required, err := decodeRequiredParameters(&document)
	if err != nil {
		return "", err
	}

	if len(required.Participants) == 0 || strings.TrimSpace(required.Participants[0].ExecutionImage) == "" {
		return "", errors.New("first participant el_image must be set")
	}
	if _, ok := required.Network.PrefundedAccounts[address]; !ok {
		return "", fmt.Errorf("network_params.prefunded_accounts must contain development wallet %q", address)
	}

	return string(payload), nil
}

func decodeRequiredParameters(document *yaml.Node) (requiredParameters, error) {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return requiredParameters{}, errors.New("parameters file must contain one YAML mapping")
	}
	var required requiredParameters
	if err := document.Decode(&required); err != nil {
		return requiredParameters{}, errors.New("parameters file must contain one YAML mapping")
	}
	return required, nil
}
