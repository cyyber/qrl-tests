package devnet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cyyber/qrl-tests/devnet/internal/kurtosis"
)

// Contracts shared with qrl-package runs: the pinned engine secret, service
// port identifiers, and service labels.
const (
	// engineJWTSecret mirrors static_files/jwt/jwtsecret in the qrl-package
	// revision pinned by packageLocator; re-verify it when bumping the pin.
	engineJWTSecret = "0xdc49981516e8e72b401a63e6405495a32dafc3939b5d6d83cc319ac0388bca1b"

	rpcPortID           = "rpc"
	webSocketPortID     = "ws"
	engineRPCPortID     = "engine-rpc"
	consensusHTTPPortID = "http"
	validatorHTTPPortID = "http-validator"
	metricsPortID       = "metrics"
	graphQLPath         = "/graphql"

	// clientTypeLabel is stamped by qrl-package itself; the qrl-tests labels
	// are stamped by the built-in parameter renderer and read back here.
	clientTypeLabel  = "qrl-package.client-type"
	participantLabel = "qrl-tests.participant"
	partitionLabel   = "qrl-tests.partition"
)

type Environment struct {
	EnclaveName     string        `json:"enclave_name"`
	Backend         Backend       `json:"backend"`
	EngineJWTSecret string        `json:"engine_jwt_secret"`
	Participants    []Participant `json:"participants"`
}

// Primary returns the lowest-indexed participant. Readiness probes and
// single-participant suites target it by convention; every resolved
// environment must have one.
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
		clientType := service.Labels[clientTypeLabel]
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

		info := ServiceInfo{Name: name, ID: service.UUID, PrivateIP: service.PrivateIP}
		switch clientType {
		case "execution":
			participant.Execution.ServiceInfo = info
			participant.Execution.RPCURL, err = service.PublicEndpoint(rpcPortID, "http")
			if err != nil {
				return nil, fmt.Errorf("execution service %q: %w", name, err)
			}
			participant.Execution.GraphQLURL = participant.Execution.RPCURL + graphQLPath
			participant.Execution.WebSocketURL, err = service.PublicEndpoint(webSocketPortID, "ws")
			if err != nil {
				return nil, fmt.Errorf("execution service %q: %w", name, err)
			}
			participant.Execution.EngineURL = optionalPublicEndpoint(service, engineRPCPortID, "http")
		case "beacon":
			participant.Consensus.ServiceInfo = info
			participant.Consensus.URL, err = service.PublicEndpoint(consensusHTTPPortID, "http")
			if err != nil {
				return nil, fmt.Errorf("consensus service %q: %w", name, err)
			}
			participant.Consensus.MetricsURL = optionalPublicEndpoint(service, metricsPortID, "http")
		case "validator":
			participant.Validator.ServiceInfo = info
			participant.Validator.URL = optionalPublicEndpoint(service, validatorHTTPPortID, "http")
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

	slices.SortFunc(participants, func(left, right Participant) int { return cmp.Compare(left.Index, right.Index) })
	return participants, nil
}

func participantIndex(name string, labels map[string]string) (int, error) {
	if value := labels[participantLabel]; value != "" {
		index, err := strconv.Atoi(value)
		if err != nil || index < 1 {
			return 0, fmt.Errorf("qrl-package service %q has invalid participant label %q", name, value)
		}
		return index, nil
	}
	return serviceIndex(name)
}

// serviceIndex falls back to qrl-package's naming convention — services are
// named like "el-1-go-qrl", with the participant index as the second segment.
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

// optionalPublicEndpoint resolves a port a service may legitimately not
// expose; absence is reported as an empty endpoint, not an error.
func optionalPublicEndpoint(service kurtosis.Service, portID, scheme string) string {
	endpoint, _ := service.PublicEndpoint(portID, scheme)
	return endpoint
}
