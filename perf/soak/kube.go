package soak

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Kube is the slice of the Kubernetes API the soak needs: which node each
// enclave pod landed on and what the kernel says each container uses. It is
// a raw REST client on purpose — client-go would triple the module graph for
// three GETs.
type Kube struct {
	BaseURL   string
	Namespace string
	token     func() (string, error)
	http      *http.Client
}

// Labels Kurtosis stamps on the pods it creates for user services.
const (
	kurtosisServiceLabel = "kurtosistech.com/id"
	// Kurtosis prefixes the labels a package sets on a service.
	kurtosisCustomLabelPrefix = "kurtosistech.com.custom/"
	participantPodLabel       = kurtosisCustomLabelPrefix + "qrl-tests.participant"

	serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
)

// InClusterKube builds a client from the projected service-account token,
// re-reading the token file on every call so rotation is transparent.
func InClusterKube(namespace string) (*Kube, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not running inside a Kubernetes cluster")
	}
	ca, err := os.ReadFile(filepath.Join(serviceAccountDir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read service account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("service account CA is not valid PEM")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	tokenPath := filepath.Join(serviceAccountDir, "token")
	return &Kube{
		BaseURL:   "https://" + host + ":" + port,
		Namespace: namespace,
		token: func() (string, error) {
			token, err := os.ReadFile(tokenPath)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(token)), nil
		},
		http: &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}, nil
}

// NewKube is the constructor for tests and out-of-cluster use.
func NewKube(baseURL, namespace, token string, client *http.Client) *Kube {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Kube{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Namespace: namespace,
		token:     func() (string, error) { return token, nil },
		http:      client,
	}
}

type Pod struct {
	Name        string
	Service     string
	Participant int
	Node        string
	Phase       string
	// Limits holds the memory limit per container in bytes; zero if unset.
	Limits map[string]float64
}

// Pods lists the enclave's user-service pods.
func (kube *Kube) Pods(ctx context.Context) ([]Pod, error) {
	var response struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				NodeName   string `json:"nodeName"`
				Containers []struct {
					Name      string `json:"name"`
					Resources struct {
						Limits map[string]string `json:"limits"`
					} `json:"resources"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	path := "/api/v1/namespaces/" + kube.Namespace + "/pods?labelSelector=" + kurtosisServiceLabel
	if err := kube.get(ctx, path, &response); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	pods := make([]Pod, 0, len(response.Items))
	for _, item := range response.Items {
		pod := Pod{
			Name:    item.Metadata.Name,
			Service: item.Metadata.Labels[kurtosisServiceLabel],
			Node:    item.Spec.NodeName,
			Phase:   item.Status.Phase,
			Limits:  make(map[string]float64, len(item.Spec.Containers)),
		}
		if value := item.Metadata.Labels[participantPodLabel]; value != "" {
			pod.Participant, _ = strconv.Atoi(value)
		}
		for _, container := range item.Spec.Containers {
			if limit := container.Resources.Limits["memory"]; limit != "" {
				pod.Limits[container.Name], _ = ParseQuantity(limit)
			}
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

// NodeLabels returns the labels of one node.
func (kube *Kube) NodeLabels(ctx context.Context, name string) (map[string]string, error) {
	var response struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := kube.get(ctx, "/api/v1/nodes/"+name, &response); err != nil {
		return nil, fmt.Errorf("get node %s: %w", name, err)
	}
	return response.Metadata.Labels, nil
}

// ContainerUsage reads metrics.k8s.io for the namespace: working set and CPU
// per container as the kubelet's cAdvisor reports them.
func (kube *Kube) ContainerUsage(ctx context.Context, pods []Pod) ([]ContainerSample, error) {
	var response struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Containers []struct {
				Name  string `json:"name"`
				Usage struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := kube.get(ctx, "/apis/metrics.k8s.io/v1beta1/namespaces/"+kube.Namespace+"/pods", &response); err != nil {
		return nil, fmt.Errorf("pod metrics: %w", err)
	}
	byName := make(map[string]Pod, len(pods))
	for _, pod := range pods {
		byName[pod.Name] = pod
	}
	var samples []ContainerSample
	for _, item := range response.Items {
		pod, known := byName[item.Metadata.Name]
		if !known {
			continue
		}
		for _, container := range item.Containers {
			memory, err := ParseQuantity(container.Usage.Memory)
			if err != nil {
				return nil, fmt.Errorf("pod %s container %s memory %q: %w", pod.Name, container.Name, container.Usage.Memory, err)
			}
			cpu, _ := ParseQuantity(container.Usage.CPU)
			samples = append(samples, ContainerSample{
				Participant:     pod.Participant,
				Pod:             pod.Name,
				Container:       container.Name,
				Node:            pod.Node,
				WorkingSetBytes: memory,
				LimitBytes:      pod.Limits[container.Name],
				CPUCores:        cpu,
			})
		}
	}
	return samples, nil
}

func (kube *Kube) get(ctx context.Context, path string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, kube.BaseURL+path, nil)
	if err != nil {
		return err
	}
	token, err := kube.token()
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := kube.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(result)
}

// PatchJobAnnotations merges annotations onto a batch/v1 Job. The heartbeat
// workflow reads these; failures must not stop the soak.
func (kube *Kube) PatchJobAnnotations(ctx context.Context, name string, annotations map[string]string) error {
	payload, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": annotations},
	})
	if err != nil {
		return err
	}
	return kube.patch(ctx, "/apis/batch/v1/namespaces/"+kube.Namespace+"/jobs/"+name, payload)
}

func (kube *Kube) patch(ctx context.Context, path string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, kube.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	token, err := kube.token()
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/merge-patch+json")
	request.Header.Set("Accept", "application/json")
	response, err := kube.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

// ParseQuantity converts a Kubernetes resource quantity to a float: bytes for
// memory, cores for CPU. It covers the suffixes the API server emits.
func ParseQuantity(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty quantity")
	}
	suffixes := []struct {
		suffix string
		scale  float64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3},
		{"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
	}
	scale := 1.0
	for _, candidate := range suffixes {
		if strings.HasSuffix(value, candidate.suffix) {
			scale = candidate.scale
			value = strings.TrimSuffix(value, candidate.suffix)
			break
		}
	}
	if exponent := strings.IndexAny(value, "eE"); exponent >= 0 {
		mantissa, err := strconv.ParseFloat(value[:exponent], 64)
		if err != nil {
			return 0, err
		}
		power, err := strconv.Atoi(value[exponent+1:])
		if err != nil {
			return 0, err
		}
		return mantissa * math.Pow10(power) * scale, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return number * scale, nil
}

// Placement is the verified node assignment of one participant.
type Placement struct {
	Participant int      `json:"participant"`
	Node        string   `json:"node"`
	NodeLabel   string   `json:"node_participant_label"`
	Pods        []string `json:"pods"`
	Pinned      bool     `json:"pinned"`
}

// VerifyPlacement checks that each participant's pods share one node and that
// the node carries that participant's label. It is the soak's proof that
// "one participant per node" actually held for this run.
func VerifyPlacement(ctx context.Context, kube *Kube, participantLabel string, participants int) ([]Placement, error) {
	pods, err := kube.Pods(ctx)
	if err != nil {
		return nil, err
	}
	byParticipant := make(map[int][]Pod)
	for _, pod := range pods {
		if pod.Participant > 0 {
			byParticipant[pod.Participant] = append(byParticipant[pod.Participant], pod)
		}
	}
	var problems []string
	placements := make([]Placement, 0, participants)
	for index := 1; index <= participants; index++ {
		group := byParticipant[index]
		placement := Placement{Participant: index}
		if len(group) == 0 {
			problems = append(problems, fmt.Sprintf("participant %d has no pods", index))
			placements = append(placements, placement)
			continue
		}
		nodes := make(map[string]bool)
		for _, pod := range group {
			placement.Pods = append(placement.Pods, pod.Name)
			nodes[pod.Node] = true
		}
		if len(nodes) != 1 {
			problems = append(problems, fmt.Sprintf("participant %d spans %d nodes", index, len(nodes)))
			placements = append(placements, placement)
			continue
		}
		placement.Node = group[0].Node
		labels, err := kube.NodeLabels(ctx, placement.Node)
		if err != nil {
			return nil, err
		}
		placement.NodeLabel = labels[participantLabel]
		placement.Pinned = placement.NodeLabel == strconv.Itoa(index)
		if !placement.Pinned {
			problems = append(problems, fmt.Sprintf("participant %d runs on %s labelled %s=%q", index, placement.Node, participantLabel, placement.NodeLabel))
		}
		placements = append(placements, placement)
	}
	if len(problems) > 0 {
		return placements, fmt.Errorf("placement: %s", strings.Join(problems, "; "))
	}
	return placements, nil
}
