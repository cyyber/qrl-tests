package soak

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Endpoints of one participant as the sampler reaches them.
type Endpoints struct {
	Index        int
	ExecutionRPC string
	ConsensusAPI string
	// Metrics maps a client to its Prometheus endpoint; missing clients are
	// not scraped.
	Metrics map[Client]string
}

// probe issues the per-participant calls of one sample. It uses plain HTTP so
// it can be tested against httptest servers and does not pull client SDKs
// into the sampler.
type probe struct {
	http *http.Client
}

const probeTimeout = 15 * time.Second

func newProbe(client *http.Client) probe {
	if client == nil {
		client = &http.Client{Timeout: probeTimeout}
	}
	return probe{http: client}
}

// head reads what is needed to pick the reference block and to judge chain
// progress, peers, and sync state.
func (p probe) head(ctx context.Context, endpoints Endpoints) ParticipantSample {
	sample := ParticipantSample{Index: endpoints.Index}

	sample.call(func() error {
		var result string
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "eth_blockNumber", nil, &result); err != nil {
			return fmt.Errorf("eth_blockNumber: %w", err)
		}
		head, err := parseHexUint(result)
		if err != nil {
			return fmt.Errorf("eth_blockNumber: %w", err)
		}
		sample.Head = head
		return nil
	})
	sample.call(func() error {
		var result string
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "net_peerCount", nil, &result); err != nil {
			return fmt.Errorf("net_peerCount: %w", err)
		}
		peers, err := parseHexUint(result)
		if err != nil {
			return fmt.Errorf("net_peerCount: %w", err)
		}
		sample.ExecutionPeers = int(peers)
		return nil
	})
	sample.call(func() error {
		var result json.RawMessage
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "eth_syncing", nil, &result); err != nil {
			return fmt.Errorf("eth_syncing: %w", err)
		}
		// false when in sync, an object while syncing.
		sample.Syncing = !bytes.Equal(bytes.TrimSpace(result), []byte("false"))
		return nil
	})

	if endpoints.ConsensusAPI == "" {
		return sample
	}
	sample.call(func() error {
		var response struct {
			Data struct {
				Header struct {
					Message struct {
						Slot string `json:"slot"`
					} `json:"message"`
				} `json:"header"`
			} `json:"data"`
		}
		if err := p.rest(ctx, endpoints.ConsensusAPI+"/eth/v1/beacon/headers/head", &response); err != nil {
			return fmt.Errorf("beacon head: %w", err)
		}
		slot, err := strconv.ParseUint(response.Data.Header.Message.Slot, 10, 64)
		if err != nil {
			return fmt.Errorf("beacon head slot %q: %w", response.Data.Header.Message.Slot, err)
		}
		sample.HeadSlot = slot
		return nil
	})
	sample.call(func() error {
		var response struct {
			Data struct {
				CurrentJustified struct {
					Epoch string `json:"epoch"`
				} `json:"current_justified"`
				Finalized struct {
					Epoch string `json:"epoch"`
				} `json:"finalized"`
			} `json:"data"`
		}
		if err := p.rest(ctx, endpoints.ConsensusAPI+"/eth/v1/beacon/states/head/finality_checkpoints", &response); err != nil {
			return fmt.Errorf("finality checkpoints: %w", err)
		}
		finalized, err := strconv.ParseUint(response.Data.Finalized.Epoch, 10, 64)
		if err != nil {
			return fmt.Errorf("finalized epoch %q: %w", response.Data.Finalized.Epoch, err)
		}
		justified, err := strconv.ParseUint(response.Data.CurrentJustified.Epoch, 10, 64)
		if err != nil {
			return fmt.Errorf("justified epoch %q: %w", response.Data.CurrentJustified.Epoch, err)
		}
		sample.FinalizedEpoch, sample.JustifiedEpoch = finalized, justified
		return nil
	})
	sample.call(func() error {
		var response struct {
			Data struct {
				Connected string `json:"connected"`
			} `json:"data"`
		}
		if err := p.rest(ctx, endpoints.ConsensusAPI+"/eth/v1/node/peer_count", &response); err != nil {
			return fmt.Errorf("peer count: %w", err)
		}
		connected, err := strconv.Atoi(response.Data.Connected)
		if err != nil {
			return fmt.Errorf("connected peers %q: %w", response.Data.Connected, err)
		}
		sample.ConsensusPeers = connected
		return nil
	})
	return sample
}

// reference fills the hash and state root of the shared reference block.
func (p probe) reference(ctx context.Context, endpoints Endpoints, sample *ParticipantSample, block uint64) {
	sample.call(func() error {
		var result struct {
			Hash      string `json:"hash"`
			StateRoot string `json:"stateRoot"`
		}
		params := []any{"0x" + strconv.FormatUint(block, 16), false}
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "eth_getBlockByNumber", params, &result); err != nil {
			return fmt.Errorf("eth_getBlockByNumber(%d): %w", block, err)
		}
		if result.Hash == "" {
			return fmt.Errorf("eth_getBlockByNumber(%d): block not found", block)
		}
		sample.ReferenceHash, sample.ReferenceState = result.Hash, result.StateRoot
		return nil
	})
}

// metrics scrapes every client's Prometheus endpoint. Scrape failures are
// recorded per client and never count as RPC errors: a missing metrics port
// is a configuration fact, not a network fault.
func (p probe) metrics(ctx context.Context, endpoints Endpoints, sample *ParticipantSample, names Thresholds) {
	if len(endpoints.Metrics) == 0 {
		return
	}
	sample.Clients = make(map[Client]ClientMetrics, len(endpoints.Metrics))
	for client, url := range endpoints.Metrics {
		families, err := p.scrape(ctx, url)
		if err != nil {
			sample.Faults = append(sample.Faults, fmt.Sprintf("%s metrics: %v", client, err))
			sample.Clients[client] = ClientMetrics{}
			continue
		}
		sample.Clients[client] = ClientMetrics{
			RSSBytes:   firstMetric(families, names.Metrics.RSS),
			HeapBytes:  firstMetric(families, names.Metrics.Heap),
			Goroutines: firstMetric(families, names.Metrics.Goroutines),
			Scraped:    true,
		}
	}
}

func (sample *ParticipantSample) call(fn func() error) {
	sample.Calls++
	if err := fn(); err != nil {
		sample.Errors++
		sample.Faults = append(sample.Faults, err.Error())
	}
}

func (p probe) rpc(ctx context.Context, url, method string, params []any, result any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		if _, ok := result.(*json.RawMessage); ok {
			*result.(*json.RawMessage) = envelope.Result
			return nil
		}
		return errors.New("empty result")
	}
	return json.Unmarshal(envelope.Result, result)
}

func (p probe) rest(ctx context.Context, url string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := p.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result)
}

// scrape parses the Prometheus text exposition format into name → first
// sample value. Labels are ignored: the metrics the gates use are process
// gauges with a single series.
func (p probe) scrape(ctx context.Context, url string) (map[string]float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := p.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return parseExposition(io.LimitReader(response.Body, 8<<20))
}

func parseExposition(reader io.Reader) (map[string]float64, error) {
	families := make(map[string]float64)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if brace := strings.IndexByte(line, '{'); brace >= 0 {
			name = line[:brace]
			closing := strings.LastIndexByte(line, '}')
			if closing < brace {
				continue
			}
			line = line[closing+1:]
		} else if space := strings.IndexAny(line, " \t"); space >= 0 {
			name = line[:space]
			line = line[space:]
		} else {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, seen := families[name]; seen {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		families[name] = value
	}
	return families, scanner.Err()
}

func firstMetric(families map[string]float64, names []string) float64 {
	for _, name := range names {
		if value, found := families[name]; found {
			return value
		}
	}
	return 0
}

func parseHexUint(value string) (uint64, error) {
	number, ok := new(big.Int).SetString(strings.TrimPrefix(value, "0x"), 16)
	if !ok || number.Sign() < 0 || !number.IsUint64() {
		return 0, fmt.Errorf("invalid hex quantity %q", value)
	}
	return number.Uint64(), nil
}
