package syncnode

import (
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/rpc"
)

// fakeEngine is an engine API that records its calls and answers with the
// given statuses.
type fakeEngine struct {
	payloadStatus, headStatus string

	calls      []string
	payload    map[string]json.RawMessage
	forkchoice map[string]common.Hash
}

func (engine *fakeEngine) NewPayloadV2(payload map[string]json.RawMessage) map[string]string {
	engine.calls = append(engine.calls, "newPayload")
	engine.payload = payload
	return map[string]string{"status": engine.payloadStatus}
}

func (engine *fakeEngine) ForkchoiceUpdatedV2(forkchoice map[string]common.Hash, attributes *json.RawMessage) map[string]any {
	engine.calls = append(engine.calls, "forkchoiceUpdated")
	engine.forkchoice = forkchoice
	return map[string]any{"payloadStatus": map[string]string{"status": engine.headStatus}}
}

// engineNode returns a node connected to the fake engine over HTTP.
func engineNode(t *testing.T, engine *fakeEngine) *Node {
	t.Helper()
	api := rpc.NewServer()
	t.Cleanup(api.Stop)
	require.NoError(t, api.RegisterName("engine", engine))
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client, err := rpc.DialContext(t.Context(), server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return &Node{Engine: client}
}

func testBlock() *types.Block {
	recipient := common.Address{7}
	header := &types.Header{
		ParentHash:  common.Hash{1},
		Coinbase:    common.Address{2},
		Root:        common.Hash{3},
		ReceiptHash: common.Hash{4},
		Number:      big.NewInt(42),
		GasLimit:    20_000_000,
		GasUsed:     21_000,
		Time:        1_700_000_000,
		Extra:       []byte("extra"),
		Random:      common.Hash{5},
		BaseFee:     big.NewInt(1_000_000_000),
	}
	transfer := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(1337), Nonce: 1, Gas: 21_000, To: &recipient, Value: big.NewInt(1),
		GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
	})
	return types.NewBlockWithHeader(header).WithBody(types.Body{
		Transactions: types.Transactions{transfer},
		Withdrawals:  types.Withdrawals{{Index: 1, Validator: 2, Address: recipient, Amount: 3}},
	})
}

func TestSyncTo(t *testing.T) {
	engine := &fakeEngine{payloadStatus: "SYNCING", headStatus: "SYNCING"}
	node := engineNode(t, engine)
	block := testBlock()

	require.NoError(t, node.syncTo(t.Context(), block))
	require.Equal(t, []string{"newPayload", "forkchoiceUpdated"}, engine.calls)

	var (
		hash         common.Hash
		number       hexutil.Uint64
		transactions []hexutil.Bytes
		withdrawals  types.Withdrawals
	)
	require.NoError(t, json.Unmarshal(engine.payload["blockHash"], &hash))
	require.NoError(t, json.Unmarshal(engine.payload["blockNumber"], &number))
	require.NoError(t, json.Unmarshal(engine.payload["transactions"], &transactions))
	require.NoError(t, json.Unmarshal(engine.payload["withdrawals"], &withdrawals))
	require.Equal(t, block.Hash(), hash)
	require.Equal(t, hexutil.Uint64(42), number)
	require.Len(t, transactions, 1)
	sent := new(types.Transaction)
	require.NoError(t, sent.UnmarshalBinary(transactions[0]))
	require.Equal(t, block.Transactions()[0].Hash(), sent.Hash())
	require.Len(t, withdrawals, 1)
	require.Equal(t, uint64(3), withdrawals[0].Amount)

	require.Equal(t, map[string]common.Hash{
		"headBlockHash":      block.Hash(),
		"safeBlockHash":      {},
		"finalizedBlockHash": {},
	}, engine.forkchoice)
}

func TestSyncToEmptyBlock(t *testing.T) {
	engine := &fakeEngine{payloadStatus: "SYNCING", headStatus: "SYNCING"}
	node := engineNode(t, engine)

	require.NoError(t, node.syncTo(t.Context(), types.NewBlockWithHeader(testBlock().Header())))
	require.JSONEq(t, "[]", string(engine.payload["withdrawals"]))
	require.JSONEq(t, "[]", string(engine.payload["transactions"]))
}

func TestSyncToInvalidBlock(t *testing.T) {
	engine := &fakeEngine{payloadStatus: "INVALID"}
	err := engineNode(t, engine).syncTo(t.Context(), testBlock())
	require.ErrorContains(t, err, "finds block 42 invalid")
	require.Equal(t, []string{"newPayload"}, engine.calls, "an invalid block is not named the head")

	engine = &fakeEngine{payloadStatus: "SYNCING", headStatus: "INVALID"}
	err = engineNode(t, engine).syncTo(t.Context(), testBlock())
	require.ErrorContains(t, err, "finds head block 42 invalid")
	require.Equal(t, []string{"newPayload", "forkchoiceUpdated"}, engine.calls)
}
