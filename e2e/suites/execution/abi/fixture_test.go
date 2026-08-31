//go:build e2e

package abi

import (
	"context"
	"math/big"

	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/e2e/suites/execution/abi/contracts"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/qrlclient"
)

type liveSuite struct {
	client      *qrlclient.Client
	wsClient    *qrlclient.Client
	from        common.Address
	signer      bind.SignerFn
	contractABI abi.ABI
	inputs      scenarioInputs
}

type liveFixture struct {
	*liveSuite
	deploymentBlock *big.Int
	address         common.Address
	contract        *bind.BoundContract
	binding         *contracts.EventEmitter
	initial         *big.Int
}

type scenarioInputs struct {
	// Large uint512 value with upper-half bits set.
	amount *big.Int

	// Negative int512 value exercising signed 64-byte encoding.
	delta *big.Int

	// Fully populated bytes64 value.
	tag [64]byte

	// 129-byte dynamic value spanning three ABI data words.
	payload []byte

	// Dynamic string crossing the 64-byte ABI word boundary.
	note string
}

func mustSucceed[T any](value T, err error) T {
	ginkgo.GinkgoHelper()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return value
}

func setupLiveSuite(ctx context.Context) *liveSuite {
	ginkgo.GinkgoHelper()

	runtime := testsuite.LoadRuntime()
	node := mustSucceed(runtime.PrimaryWithWebSocket(ctx))

	transactor := mustSucceed(bind.NewKeyedTransactorWithChainID(node.Wallet, node.ChainID))

	inputs := scenarioInputs{
		amount: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 511), big.NewInt(0x1234)),
		delta:  new(big.Int).Add(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 510)), big.NewInt(42)),
		note:   "VM string crosses the 64-byte ABI word boundary: 0123456789abcdef0123456789abcdef",
	}
	for index := range inputs.tag {
		inputs.tag[index] = byte(0x80 + index)
	}

	inputs.payload = patternedBytes(129, 7)

	parsed := mustSucceed(contracts.EventEmitterMetaData.GetAbi())

	return &liveSuite{
		client:      node.Execution,
		wsClient:    node.ExecutionWebSocket,
		from:        transactor.From,
		signer:      transactor.Signer,
		contractABI: *parsed,
		inputs:      inputs,
	}
}

func (suite *liveSuite) deployEventEmitter(ctx context.Context) *liveFixture {
	ginkgo.GinkgoHelper()

	deploymentAuth := suite.transactOpts(ctx)
	initial := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 500), big.NewInt(1337))
	deploymentNote := "dynamic constructor value: " + suite.inputs.note
	deploymentPayload := suite.inputs.payload
	deploymentRecord := contracts.EventEmitterRecord{
		Amount:    suite.inputs.amount,
		Recipient: suite.from,
		Tag:       suite.inputs.tag,
	}
	deploymentNumbers := []uint16{0, 1, 0xffff, 0x1234}
	address, tx, binding, err := contracts.DeployEventEmitter(
		deploymentAuth,
		suite.client,
		initial,
		deploymentNote,
		deploymentPayload,
		deploymentRecord,
		deploymentNumbers,
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	receipt := suite.waitSuccessfulTransaction(ctx, tx)
	gomega.Expect(receipt.ContractAddress).To(gomega.Equal(address))
	gomega.Expect(receipt.Logs).To(gomega.HaveLen(1))

	deployed := suite.contractABI.Events["Deployed"]
	log := receipt.Logs[0]
	gomega.Expect(log.Topics).To(gomega.Equal([]common.LogTopic{
		common.HashToLogTopic(deployed.ID),
	}))

	wantDeploymentData, err := deployed.Inputs.Pack(
		initial,
		deploymentNote,
		deploymentPayload,
		deploymentRecord,
		deploymentNumbers,
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pack canonical Deployed event data")
	gomega.Expect(log.Data).To(gomega.Equal(wantDeploymentData))

	deployedEvent := mustSucceed(binding.ParseDeployed(*log))
	gomega.Expect(deployedEvent.Value).To(gomega.Equal(initial))
	gomega.Expect(deployedEvent.Note).To(gomega.Equal(deploymentNote))
	gomega.Expect(deployedEvent.Payload).To(gomega.Equal(deploymentPayload))
	gomega.Expect(deployedEvent.Record).To(gomega.Equal(deploymentRecord))
	gomega.Expect(deployedEvent.Numbers).To(gomega.Equal(deploymentNumbers))

	return &liveFixture{
		liveSuite:       suite,
		deploymentBlock: receipt.BlockNumber,
		address:         address,
		contract: bind.NewBoundContract(
			address,
			suite.contractABI,
			suite.client,
			suite.client,
			suite.client,
		),
		binding: binding,
		initial: initial,
	}
}

func (suite *liveSuite) transactOpts(ctx context.Context) *bind.TransactOpts {
	return &bind.TransactOpts{
		From:    suite.from,
		Signer:  suite.signer,
		Context: ctx,
	}
}

func (fixture *liveFixture) callOpts(ctx context.Context) *bind.CallOpts {
	return &bind.CallOpts{
		Context:     ctx,
		From:        fixture.from,
		BlockNumber: fixture.deploymentBlock,
	}
}

func (suite *liveSuite) waitTransaction(
	ctx context.Context,
	tx *types.Transaction,
) *types.Receipt {
	ginkgo.GinkgoHelper()

	receipt, err := bind.WaitMined(ctx, suite.client, tx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "wait for transaction %s", tx.Hash())
	gomega.Expect(receipt).NotTo(gomega.BeNil(), "transaction %s has no mined receipt", tx.Hash())
	gomega.Expect(receipt.BlockNumber).NotTo(gomega.BeNil(), "transaction %s has no block number", tx.Hash())
	return receipt
}

func (suite *liveSuite) waitSuccessfulTransaction(
	ctx context.Context,
	tx *types.Transaction,
) *types.Receipt {
	ginkgo.GinkgoHelper()

	receipt := suite.waitTransaction(ctx, tx)
	gomega.Expect(receipt.Status).To(
		gomega.Equal(types.ReceiptStatusSuccessful),
		"transaction %s status",
		tx.Hash(),
	)
	return receipt
}
