package devnet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
)

type Environment struct {
	EnclaveName     string        `json:"enclave_name"`
	Backend         Backend       `json:"backend"`
	EngineJWTSecret string        `json:"engine_jwt_secret"`
	Participants    []Participant `json:"participants"`
}

func (environment Environment) Primary() (Participant, error) {
	if len(environment.Participants) == 0 {
		return Participant{}, errors.New("environment has no participants")
	}
	return environment.Participants[0], nil
}

type Participant struct {
	Index     int              `json:"index"`
	Execution ExecutionService `json:"execution"`
	Consensus ConsensusService `json:"consensus"`
	Validator ValidatorService `json:"validator"`
}

type ServiceInfo struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	PrivateIP string `json:"private_ip"`
}

type ExecutionService struct {
	ServiceInfo
	RPCURL       string `json:"rpc_url"`
	GraphQLURL   string `json:"graphql_url"`
	WebSocketURL string `json:"websocket_url"`
	EngineURL    string `json:"engine_url"`
}

type ConsensusService struct {
	ServiceInfo
	URL        string `json:"url"`
	MetricsURL string `json:"metrics_url"`
}

type ValidatorService struct {
	ServiceInfo
	URL        string `json:"url"`
	MetricsURL string `json:"metrics_url"`
}

func resolveEnvironment(ctx context.Context, client kurtosisClient, name string, backend Backend) (Environment, error) {
	services, err := client.Services(ctx, name)
	if err != nil {
		return Environment{}, err
	}
	participants, err := participantsFromServices(services)
	if err != nil {
		return Environment{}, err
	}
	return Environment{
		EnclaveName:     name,
		Backend:         backend,
		EngineJWTSecret: engineJWTSecret,
		Participants:    participants,
	}, nil
}

func participantsFromServices(services map[string]kurtosis.Service) ([]Participant, error) {
	byIndex := make(map[int]*Participant)
	for name, service := range services {
		clientType := service.Labels["qrl-package.client-type"]
		if clientType != "execution" && clientType != "beacon" && clientType != "validator" {
			continue
		}
		index, err := participantIndex(name, service.Labels)
		if err != nil {
			return nil, err
		}
		participant := byIndex[index]
		if participant == nil {
			participant = &Participant{Index: index}
			byIndex[index] = participant
		}
		switch clientType {
		case "execution":
			participant.Execution.ServiceInfo = ServiceInfo{Name: name, ID: service.UUID, PrivateIP: service.PrivateIP}
			participant.Execution.RPCURL, err = service.PublicEndpoint(rpcPortID, "http")
			if err != nil {
				return nil, fmt.Errorf("execution service %q: %w", name, err)
			}
			participant.Execution.GraphQLURL = participant.Execution.RPCURL + graphQLPath
			participant.Execution.WebSocketURL, err = service.PublicEndpoint(webSocketPortID, "ws")
			if err != nil {
				return nil, fmt.Errorf("execution service %q: %w", name, err)
			}
			participant.Execution.EngineURL = optionalPublicEndpoint(service, "engine-rpc", "http")
		case "beacon":
			participant.Consensus.ServiceInfo = ServiceInfo{Name: name, ID: service.UUID, PrivateIP: service.PrivateIP}
			participant.Consensus.URL, err = service.PublicEndpoint(consensusHTTPPortID, "http")
			if err != nil {
				return nil, fmt.Errorf("consensus service %q: %w", name, err)
			}
			participant.Consensus.MetricsURL = optionalPublicEndpoint(service, metricsPortID, "http")
		case "validator":
			participant.Validator.ServiceInfo = ServiceInfo{Name: name, ID: service.UUID, PrivateIP: service.PrivateIP}
			participant.Validator.URL = optionalPublicEndpoint(service, "http-validator", "http")
			participant.Validator.MetricsURL = optionalPublicEndpoint(service, metricsPortID, "http")
		}
	}
	if len(byIndex) == 0 {
		return nil, errors.New("no qrl-package participants found")
	}
	participants := make([]Participant, 0, len(byIndex))
	for _, participant := range byIndex {
		if participant.Execution.RPCURL == "" || participant.Consensus.URL == "" {
			return nil, fmt.Errorf("participant %d is missing an execution or consensus endpoint", participant.Index)
		}
		participants = append(participants, *participant)
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i].Index < participants[j].Index })
	return participants, nil
}

func participantIndex(name string, labels map[string]string) (int, error) {
	if value := labels["qrl-tests.participant"]; value != "" {
		index, err := strconv.Atoi(value)
		if err != nil || index < 1 {
			return 0, fmt.Errorf("qrl-package service %q has invalid participant label %q", name, value)
		}
		return index, nil
	}
	return serviceIndex(name)
}

func serviceIndex(name string) (int, error) {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return 0, fmt.Errorf("qrl-package service %q has no participant index", name)
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 1 {
		return 0, fmt.Errorf("qrl-package service %q has invalid participant index", name)
	}
	return index, nil
}

func optionalPublicEndpoint(service kurtosis.Service, portID, scheme string) string {
	endpoint, _ := service.PublicEndpoint(portID, scheme)
	return endpoint
}

type Backend string

type Capability string

const (
	BackendDocker     Backend = "docker"
	BackendKubernetes Backend = "kubernetes"

	CapabilityNetworkPartition Capability = "network-partition"
)

func (backend Backend) Supports(capability Capability) bool {
	switch capability {
	case CapabilityNetworkPartition:
		return backend == BackendDocker
	default:
		return false
	}
}

func ParseBackend(value string) (Backend, error) {
	backend := Backend(strings.TrimSpace(value))
	if backend == "" {
		return BackendDocker, nil
	}
	switch backend {
	case BackendDocker, BackendKubernetes:
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported Kurtosis backend %q", value)
	}
}

type Images struct {
	Execution string
	Clef      string
	Consensus string
	Validator string
	Genesis   string
}

func DefaultImages() Images {
	return Images{
		Execution: DefaultExecutionImage,
		Clef:      DefaultClefImage,
		Consensus: DefaultConsensusImage,
		Validator: DefaultValidatorImage,
		Genesis:   DefaultGenesisImage,
	}
}

func (images Images) withDefaults() Images {
	defaults := DefaultImages()
	if images.Execution == "" {
		images.Execution = defaults.Execution
	}
	if images.Clef == "" {
		images.Clef = defaults.Clef
	}
	if images.Consensus == "" {
		images.Consensus = defaults.Consensus
	}
	if images.Validator == "" {
		images.Validator = defaults.Validator
	}
	if images.Genesis == "" {
		images.Genesis = defaults.Genesis
	}
	return images
}

func (images Images) validate(backend Backend) error {
	for _, item := range []struct {
		name, image string
	}{
		{"execution", images.Execution},
		{"Clef", images.Clef},
		{"consensus", images.Consensus},
		{"validator", images.Validator},
		{"genesis", images.Genesis},
	} {
		if strings.TrimSpace(item.image) == "" {
			return fmt.Errorf("%s image is empty", item.name)
		}
		if backend == BackendKubernetes && strings.HasPrefix(item.image, "local/") {
			return fmt.Errorf("%s image %q is not available to Kubernetes; use a registry image", item.name, item.image)
		}
	}
	return nil
}
