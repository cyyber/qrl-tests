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
// progress, peers, and sync state. go-qrl and Qrysm use the qrl_ JSON-RPC
// namespace and /qrl/v1 beacon paths, not the Ethereum eth_ / /eth/v1 names.

func (p probe) head(ctx context.Context, endpoints Endpoints) ParticipantSample {
	sample := ParticipantSample{Index: endpoints.Index}

	sample.call(func() error {
		var result string
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "qrl_blockNumber", nil, &result); err != nil {
			return fmt.Errorf("qrl_blockNumber: %w", err)
		}
		head, err := parseHexUint(result)
		if err != nil {
			return fmt.Errorf("qrl_blockNumber: %w", err)
		}
		sample.Head = head
		return nil
	})
	sample.call(func() error {
		var result string
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "qrl_getBlockTransactionCountByNumber", []any{"latest"}, &result); err != nil {
			return fmt.Errorf("qrl_getBlockTransactionCountByNumber: %w", err)
		}
		count, err := parseHexUint(result)
		if err != nil {
			return fmt.Errorf("qrl_getBlockTransactionCountByNumber: %w", err)
		}
		sample.TxInHead = count
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
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "qrl_syncing", nil, &result); err != nil {
			return fmt.Errorf("qrl_syncing: %w", err)
		}
		// false when in sync, an object while syncing.
		sample.Syncing = !bytes.Equal(bytes.TrimSpace(result), []byte("false"))
		return nil
	})

	if endpoints.ConsensusAPI == "" {
		return sample
	}
	sample.call(func() error {
		slot, err := p.beaconHeadSlot(ctx, endpoints.ConsensusAPI)
		if err != nil {
			return fmt.Errorf("beacon head: %w", err)
		}
		sample.HeadSlot = slot
		return nil
	})
	sample.call(func() error {
		finalized, justified, err := p.beaconFinality(ctx, endpoints.ConsensusAPI, sample.HeadSlot)
		if err != nil {
			return fmt.Errorf("finality checkpoints: %w", err)
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
		if err := p.rest(ctx, endpoints.ConsensusAPI+"/qrl/v1/node/peer_count", &response); err != nil {
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

const defaultSlotsPerEpoch = 8

func (p probe) beaconHeadSlot(ctx context.Context, base string) (uint64, error) {
	var header struct {
		Data struct {
			Header struct {
				Message struct {
					Slot string `json:"slot"`
				} `json:"message"`
			} `json:"header"`
		} `json:"data"`
	}
	if err := p.rest(ctx, base+"/qrl/v1/beacon/headers/head", &header); err == nil {
		return parseDecimalUint(header.Data.Header.Message.Slot, "beacon head slot")
	}

	var block struct {
		Data struct {
			Message struct {
				Slot string `json:"slot"`
			} `json:"message"`
			ZondBlock *struct {
				Slot string `json:"slot"`
			} `json:"zond_block"`
		} `json:"data"`
	}
	if err := p.rest(ctx, base+"/qrl/v1/beacon/blocks/head", &block); err == nil {
		if block.Data.ZondBlock != nil && block.Data.ZondBlock.Slot != "" {
			return parseDecimalUint(block.Data.ZondBlock.Slot, "beacon head slot")
		}
		return parseDecimalUint(block.Data.Message.Slot, "beacon head slot")
	}

	var heads struct {
		Data []struct {
			Slot string `json:"slot"`
		} `json:"data"`
	}
	if err := p.rest(ctx, base+"/qrl/v1/debug/beacon/heads", &heads); err != nil {
		return 0, err
	}
	if len(heads.Data) == 0 {
		return 0, errors.New("debug beacon heads returned no heads")
	}
	return parseDecimalUint(heads.Data[0].Slot, "beacon head slot")
}

func (p probe) beaconFinality(ctx context.Context, base string, headSlot uint64) (uint64, uint64, error) {
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
	if err := p.rest(ctx, base+"/qrl/v1/beacon/states/head/finality_checkpoints", &response); err == nil {
		finalized, err := parseDecimalUint(response.Data.Finalized.Epoch, "finalized epoch")
		if err != nil {
			return 0, 0, err
		}
		justified, err := parseDecimalUint(response.Data.CurrentJustified.Epoch, "justified epoch")
		if err != nil {
			return 0, 0, err
		}
		return finalized, justified, nil
	}
	// Older Qrysm builds omit the checkpoints route. Protocol finality lags
	// the head by two epochs once the head is past that.
	headEpoch := headSlot / defaultSlotsPerEpoch
	if headEpoch >= 2 {
		return headEpoch - 2, headEpoch - 1, nil
	}
	return 0, 0, nil
}

func parseDecimalUint(value, label string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", label, value, err)
	}
	return parsed, nil
}

// reference fills the hash and state root of the shared reference block.
func (p probe) reference(ctx context.Context, endpoints Endpoints, sample *ParticipantSample, block uint64) {
	sample.call(func() error {
		var result struct {
			Hash      string `json:"hash"`
			StateRoot string `json:"stateRoot"`
		}
		params := []any{"0x" + strconv.FormatUint(block, 16), false}
		if err := p.rpc(ctx, endpoints.ExecutionRPC, "qrl_getBlockByNumber", params, &result); err != nil {
			return fmt.Errorf("qrl_getBlockByNumber(%d): %w", block, err)
		}
		if result.Hash == "" {
			return fmt.Errorf("qrl_getBlockByNumber(%d): block not found", block)
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
			OpenFDs:    firstMetric(families, names.Metrics.OpenFDs),
			GCPauseSec: firstMetric(families, names.Metrics.GCPause),
			GCCount:    firstMetric(families, names.Metrics.GCCount),
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
		labels := ""
		if brace := strings.IndexByte(line, '{'); brace >= 0 {
			name = line[:brace]
			closing := strings.LastIndexByte(line, '}')
			if closing < brace {
				continue
			}
			labels = line[brace : closing+1]
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
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		if labels != "" {
			families[name+labels] = value
		}
		if _, seen := families[name]; seen {
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
