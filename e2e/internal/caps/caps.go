// Package caps checks what a suite observed against the limits of the
// clients: a size against the cap it has to fit, a duration against its
// budget.
package caps

import (
	"fmt"
	"time"
)

// Cap is a limit of a client.
type Cap struct {
	Name string
	// Limit is in bytes, or in nanoseconds for a time budget.
	Limit uint64
}

// The limits of go-qrl and qrysm that suites check. They are copies: neither
// client serves them, so a change in a client needs the same change here.
var (
	// QRLMessage is the largest qrl protocol message a go-qrl peer accepts:
	// maxMessageSize in qrl/protocols/qrl.
	QRLMessage = Cap{Name: "qrl protocol message", Limit: 10 << 20}
	// BeaconGossip is the largest gossip message a qrysm peer accepts,
	// uncompressed: GossipMaxSize.
	BeaconGossip = Cap{Name: "beacon gossip message", Limit: 10 << 20}
	// BeaconChunk is the largest response chunk a qrysm peer accepts,
	// uncompressed: MaxChunkSize.
	BeaconChunk = Cap{Name: "beacon response chunk", Limit: 10 << 20}
	// RPCBatchResponse is the largest reply go-qrl sends to a batch of JSON-RPC
	// calls: BatchResponseMaxSize in node.
	RPCBatchResponse = Cap{Name: "JSON-RPC batch response", Limit: 25_000_000}
	// RPCWebSocketMessage is the largest WebSocket message a go-qrl RPC client
	// or server reads: wsDefaultReadLimit in rpc.
	RPCWebSocketMessage = Cap{Name: "JSON-RPC WebSocket message", Limit: 32 << 20}
	// EngineRequestBody is the largest request body go-qrl's engine API
	// accepts: engineAPIBodyLimit in node.
	EngineRequestBody = Cap{Name: "engine API request body", Limit: 128 << 20}
	// RPCCall is how long go-qrl runs a qrl_call or a gas estimate before it
	// gives up: RPCQRVMTimeout in qrlconfig.
	RPCCall = Cap{Name: "JSON-RPC call time", Limit: uint64(5 * time.Second)}
	// RPCTrace is how long go-qrl traces a call or a transaction before it
	// gives up: defaultTraceTimeout in qrl/tracers.
	RPCTrace = Cap{Name: "JSON-RPC trace time", Limit: uint64(5 * time.Second)}
	// RPCWrite is how long go-qrl's HTTP server takes to write a reply
	// before it closes the connection: WriteTimeout in rpc.
	RPCWrite = Cap{Name: "JSON-RPC reply write", Limit: uint64(30 * time.Second)}
	// RPCWebSocketWrite is how long go-qrl takes to write a reply or a
	// notification to a WebSocket before it closes the connection:
	// defaultWriteTimeout in rpc.
	RPCWebSocketWrite = Cap{Name: "JSON-RPC WebSocket write", Limit: uint64(10 * time.Second)}
	// QRLMessageWrite is how long go-qrl takes to write a protocol message
	// before it drops the peer: frameWriteTimeout in p2p.
	QRLMessageWrite = Cap{Name: "qrl protocol message write", Limit: uint64(20 * time.Second)}
	// BeaconResponse is how long a qrysm peer takes to write a whole
	// response, of up to 64 blocks, before the stream is reset: RESP_TIMEOUT.
	BeaconResponse = Cap{Name: "beacon response write", Limit: uint64(50 * time.Second)}
	// BeaconGossipValidation is how long qrysm takes to validate a gossip
	// message before it gives up on it: pubsubMessageTimeout in sync.
	BeaconGossipValidation = Cap{Name: "beacon gossip validation", Limit: uint64(30 * time.Second)}
	// EngineGetPayload is how long qrysm waits for the execution client to
	// hand over the block it built: defaultEngineTimeout in execution.
	EngineGetPayload = Cap{Name: "engine API getPayload call", Limit: uint64(10 * time.Second)}
	// EnginePayload is how long qrysm waits for the execution client to
	// answer newPayload or forkchoiceUpdated: ExecutionEngineTimeoutValue.
	EnginePayload = Cap{Name: "engine API payload call", Limit: uint64(8 * time.Second)}
)

// Class is how strictly an observed value is held to its cap.
type Class int

const (
	// WorstCase is for a value proven to be the largest possible. It fails
	// only above the cap, and is tight with less than a tenth to spare.
	WorstCase Class = iota
	// Measured is for the largest value a run observed, when a larger one
	// may exist. It fails with less than a tenth of the cap to spare.
	Measured
	// TimeBudget is for a duration, which varies with the machine. It fails
	// above half of the cap.
	TimeBudget
)

type Verdict int

const (
	Pass Verdict = iota
	// Tight passes, with little to spare.
	Tight
	Fail
)

func (verdict Verdict) String() string {
	return [...]string{"pass", "tight", "fail"}[verdict]
}

// Result is an observed value checked against a cap.
type Result struct {
	Cap      Cap
	Class    Class
	Observed uint64
	Verdict  Verdict
}

// Check holds observed to the cap as strictly as class asks.
func (limit Cap) Check(class Class, observed uint64) Result {
	var (
		over      = observed > limit.Limit
		tenthLeft = observed*10 <= limit.Limit*9
		halfLeft  = observed*2 <= limit.Limit
		verdict   = Pass
	)
	switch class {
	case WorstCase:
		if over {
			verdict = Fail
		} else if !tenthLeft {
			verdict = Tight
		}
	case Measured:
		if !tenthLeft {
			verdict = Fail
		}
	case TimeBudget:
		if !halfLeft {
			verdict = Fail
		}
	}
	return Result{Cap: limit, Class: class, Observed: observed, Verdict: verdict}
}

// CheckDuration holds observed to the cap as a time budget.
func (limit Cap) CheckDuration(observed time.Duration) Result {
	return limit.Check(TimeBudget, uint64(max(observed, 0)))
}

// MinBandwidth returns the slowest link, in megabits per second, that carries
// size bytes within the cap, for a cap that is a time limit.
func (limit Cap) MinBandwidth(size uint64) float64 {
	return float64(size) * 8 / 1e6 / time.Duration(limit.Limit).Seconds()
}

// Spare returns the share of the cap that the observed value leaves, which is
// negative above the cap.
func (result Result) Spare() float64 {
	return 1 - float64(result.Observed)/float64(result.Cap.Limit)
}

func (result Result) String() string {
	observed, limit := fmt.Sprint(result.Observed), fmt.Sprintf("%d bytes", result.Cap.Limit)
	if result.Class == TimeBudget {
		observed, limit = time.Duration(result.Observed).String(), time.Duration(result.Cap.Limit).String()
	}
	spare := fmt.Sprintf("%.1f%% to spare", 100*result.Spare())
	if result.Observed > result.Cap.Limit {
		spare = "above the cap"
	}
	return fmt.Sprintf("%s: %s of %s, %s: %s", result.Cap.Name, observed, limit, spare, result.Verdict)
}
