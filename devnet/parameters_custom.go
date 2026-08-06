package devnet

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

func customParameters(payload []byte, address string, backend Backend) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return "", errors.New("parameters file must contain one YAML mapping")
	}
	shape, err := decodeParameterShape(&document)
	if err != nil {
		return "", err
	}
	if len(shape.Participants) == 0 || strings.TrimSpace(shape.Participants[0].ExecutionImage) == "" {
		return "", errors.New("first participant el_image must be set")
	}
	if _, ok := shape.Network.PrefundedAccounts[address]; !ok {
		return "", fmt.Errorf("network_params.prefunded_accounts must contain development wallet %q", address)
	}
	if err := shape.validateImages(backend); err != nil {
		return "", err
	}
	return string(payload), nil
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

func (shape parameterShape) validateImages(backend Backend) error {
	if backend != BackendKubernetes {
		return nil
	}
	for index, participant := range shape.Participants {
		for _, item := range []struct {
			name, image string
		}{
			{"execution", participant.ExecutionImage},
			{"Clef", participant.RemoteSignerImage},
			{"consensus", participant.ConsensusImage},
			{"validator", participant.ValidatorImage},
		} {
			if strings.HasPrefix(strings.TrimSpace(item.image), "local/") {
				return fmt.Errorf("participant %d %s image %q is not available to Kubernetes; use a registry image", index+1, item.name, item.image)
			}
		}
	}
	if strings.HasPrefix(strings.TrimSpace(shape.Genesis.Image), "local/") {
		return fmt.Errorf("genesis image %q is not available to Kubernetes; use a registry image", shape.Genesis.Image)
	}
	return nil
}
