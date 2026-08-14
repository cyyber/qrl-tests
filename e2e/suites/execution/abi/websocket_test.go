//go:build e2e

package abi

import (
	"context"
	"time"

	"github.com/cyyber/qrl-tests/e2e/internal/abifixture"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/theQRL/go-qrl/accounts/abi/bind"
	"github.com/theQRL/go-qrl/crypto"
	"github.com/theQRL/go-qrl/event"
)

const watchDeliveryTimeout = 90 * time.Second

// awaitEvent waits for one delivery on a generated watcher channel and hands
// it to assert, failing on subscription errors, timeout, or spec cancellation.
func awaitEvent[Event any](
	ctx context.Context,
	what string,
	events <-chan *Event,
	subscription event.Subscription,
	assert func(*Event),
) {
	ginkgo.GinkgoHelper()

	select {
	case received, open := <-events:
		gomega.Expect(open).To(gomega.BeTrue(), "%s event channel closed", what)
		gomega.Expect(received).NotTo(gomega.BeNil())
		assert(received)
	case err, open := <-subscription.Err():
		gomega.Expect(open).To(gomega.BeTrue(), "%s subscription closed", what)
		gomega.Expect(err).NotTo(gomega.BeNil(), "%s subscription closed without an error", what)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	case <-time.After(watchDeliveryTimeout):
		ginkgo.Fail("timed out waiting for filtered " + what + " event")
	case <-ctx.Done():
		gomega.Expect(ctx.Err()).NotTo(gomega.HaveOccurred())
	}
}

func (fixture *liveFixture) assertWebSocketWatcher(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Hyperion:
	// event IndexedScalars(bool indexed flag, bytes5 indexed code, int16 indexed delta);
	// function emitIndexedScalars(bool flag, bytes5 code, int16 delta) external {
	//     emit IndexedScalars(flag, code, delta);
	// }
	// Goal: the generated WebSocket watcher ignores an event that fails one
	// indexed-topic rule, then delivers and decodes the event matching all rules.
	ginkgo.By("watching a filtered event through the generated WebSocket binding")
	auth := fixture.transactOpts(ctx)
	watched := mustSucceed(abifixture.NewEventEmitter(fixture.address, fixture.wsClient))
	events := make(chan *abifixture.EventEmitterIndexedScalars, 1)
	code, delta := [5]byte{1, 2, 3, 4, 5}, int16(-777)
	subscription := mustSucceed(watched.WatchIndexedScalars(
		&bind.WatchOpts{Context: ctx},
		events,
		[]bool{false},
		[][5]byte{code},
		[]int16{delta},
	))
	ginkgo.DeferCleanup(subscription.Unsubscribe)

	nonMatchingTx := mustSucceed(fixture.binding.EmitIndexedScalars(
		auth,
		true,
		code,
		delta,
	))
	fixture.waitSuccessfulTransaction(ctx, nonMatchingTx)
	matchingTx := mustSucceed(fixture.binding.EmitIndexedScalars(auth, false, code, delta))
	receipt := fixture.waitSuccessfulTransaction(ctx, matchingTx)

	awaitEvent(ctx, "generated IndexedScalars", events, subscription, func(received *abifixture.EventEmitterIndexedScalars) {
		gomega.Expect(received.Raw.TxHash).To(gomega.Equal(receipt.TxHash))
		gomega.Expect(received.Raw.Address).To(gomega.Equal(fixture.address))
		gomega.Expect(received.Flag).To(gomega.BeFalse())
		gomega.Expect(received.Code).To(gomega.Equal(code))
		gomega.Expect(received.Delta).To(gomega.Equal(delta))
	})

	// Hyperion:
	// event Dynamic(bytes indexed payload, string indexed note, uint512 amount);
	// function store(...) external { emit Dynamic(payload, note, amount); }
	// Goal: the generated WebSocket watcher hashes the original dynamic filter
	// values, rejects a non-matching event, and decodes the matching hashes.
	ginkgo.By("watching indexed dynamic values through the generated WebSocket binding")
	dynamicEvents := make(chan *abifixture.EventEmitterDynamic, 1)
	dynamicSubscription := mustSucceed(watched.WatchDynamic(
		&bind.WatchOpts{Context: ctx},
		dynamicEvents,
		[][]byte{fixture.inputs.payload},
		[]string{fixture.inputs.note},
	))
	ginkgo.DeferCleanup(dynamicSubscription.Unsubscribe)

	nonMatchingPayload := []byte("not the watched payload")
	nonMatchingDynamicTx := mustSucceed(fixture.binding.Store(
		auth,
		fixture.inputs.amount,
		fixture.inputs.delta,
		fixture.inputs.tag,
		fixture.from,
		nonMatchingPayload,
		fixture.inputs.note,
		true,
	))
	fixture.waitSuccessfulTransaction(ctx, nonMatchingDynamicTx)
	matchingDynamicTx := mustSucceed(fixture.binding.Store(
		auth,
		fixture.inputs.amount,
		fixture.inputs.delta,
		fixture.inputs.tag,
		fixture.from,
		fixture.inputs.payload,
		fixture.inputs.note,
		true,
	))
	dynamicReceipt := fixture.waitSuccessfulTransaction(ctx, matchingDynamicTx)
	payloadHash := crypto.Keccak256Hash(fixture.inputs.payload)
	noteHash := crypto.Keccak256Hash([]byte(fixture.inputs.note))

	awaitEvent(ctx, "generated Dynamic", dynamicEvents, dynamicSubscription, func(received *abifixture.EventEmitterDynamic) {
		gomega.Expect(received.Raw.TxHash).To(gomega.Equal(dynamicReceipt.TxHash))
		gomega.Expect(received.Raw.Address).To(gomega.Equal(fixture.address))
		gomega.Expect(received.Payload).To(gomega.Equal(payloadHash))
		gomega.Expect(received.Note).To(gomega.Equal(noteHash))
		gomega.Expect(received.Amount).To(gomega.Equal(fixture.inputs.amount))
	})
}
