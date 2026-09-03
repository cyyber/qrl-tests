package soak

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// Sample is one sampling round across the network. Samples are appended to
// samples.jsonl as they are taken so a killed run still leaves evidence.
type Sample struct {
	At time.Time `json:"at"`
	// Phase is "warmup" until the warm-up criteria hold, then "steady".
	Phase        Phase               `json:"phase"`
	Participants []ParticipantSample `json:"participants"`
	// Reference is the block every participant was asked to describe for the
	// consensus-split check: min(head) - 2 at the time of the sample.
	Reference uint64 `json:"reference_block,omitempty"`
	Canary    *Canary `json:"canary,omitempty"`
	// Containers holds cgroup usage from metrics.k8s.io; Kubernetes only.
	Containers []ContainerSample `json:"containers,omitempty"`
}

type Phase string

const (
	PhaseWarmup Phase = "warmup"
	PhaseSteady Phase = "steady"
)

type ParticipantSample struct {
	Index int `json:"index"`

	// Execution layer.
	Head           uint64 `json:"head"`
	ReferenceHash  string `json:"reference_hash,omitempty"`
	ReferenceState string `json:"reference_state_root,omitempty"`
	ExecutionPeers int    `json:"execution_peers"`
	Syncing        bool   `json:"syncing"`

	// Consensus layer.
	HeadSlot       uint64 `json:"head_slot"`
	FinalizedEpoch uint64 `json:"finalized_epoch"`
	JustifiedEpoch uint64 `json:"justified_epoch"`
	ConsensusPeers int    `json:"consensus_peers"`

	// Probe accounting; every RPC/REST call made for this sample.
	Calls  int      `json:"calls"`
	Errors int      `json:"errors"`
	Faults []string `json:"faults,omitempty"`

	Clients map[Client]ClientMetrics `json:"clients,omitempty"`
}

type Client string

const (
	ClientExecution Client = "execution"
	ClientConsensus Client = "consensus"
	ClientValidator Client = "validator"
)

// ClientMetrics is what the client itself reports about its process.
type ClientMetrics struct {
	RSSBytes   float64 `json:"rss_bytes,omitempty"`
	HeapBytes  float64 `json:"heap_bytes,omitempty"`
	Goroutines float64 `json:"goroutines,omitempty"`
	Scraped    bool    `json:"scraped"`
}

// ContainerSample is what the kernel reports about the container.
type ContainerSample struct {
	Participant     int     `json:"participant,omitempty"`
	Pod             string  `json:"pod"`
	Container       string  `json:"container"`
	Node            string  `json:"node,omitempty"`
	WorkingSetBytes float64 `json:"working_set_bytes"`
	LimitBytes      float64 `json:"limit_bytes,omitempty"`
	CPUCores        float64 `json:"cpu_cores,omitempty"`
}

// Canary is the outcome of one deterministic transfer sent at sampling time.
type Canary struct {
	SentAt   time.Time     `json:"sent_at"`
	Latency  time.Duration `json:"latency,omitempty"`
	Included bool          `json:"included"`
	Block    uint64        `json:"block,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// ReadSamples decodes a samples.jsonl stream.
func ReadSamples(reader io.Reader) ([]Sample, error) {
	var samples []Sample
	decoder := json.NewDecoder(reader)
	for decoder.More() {
		var sample Sample
		if err := decoder.Decode(&sample); err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func bytesReader(payload []byte) io.Reader { return bytes.NewReader(payload) }
