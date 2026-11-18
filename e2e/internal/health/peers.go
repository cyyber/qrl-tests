// Package health checks that a suite's load leaves the network healthy.
package health

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/theQRL/go-qrl/p2p"
	"github.com/theQRL/go-qrl/p2p/qnode"
	"github.com/theQRL/go-qrl/rpc"
)

// PeerDrop is a peer whose connection to the node ended.
type PeerDrop struct {
	Received      time.Time
	Peer          qnode.ID
	RemoteAddress string
	Reason        string
}

// PeerWatch records the peers an execution node drops.
type PeerWatch struct {
	client       *rpc.Client
	subscription *rpc.ClientSubscription
	stopped      chan struct{}

	mutex  sync.Mutex
	drops  []PeerDrop
	err    error
	closed bool
}

const (
	startTimeout = 10 * time.Second
	checkTimeout = 10 * time.Second
)

// WatchPeers records the peers dropped by the node at wsURL until Close.
func WatchPeers(ctx context.Context, wsURL string) (*PeerWatch, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	client, err := rpc.DialContext(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	events := make(chan *p2p.PeerEvent, 64)
	subscription, err := client.Subscribe(ctx, "admin", events, "peerEvents")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("subscribe to the peer events of %s: %w", wsURL, err)
	}

	watch := &PeerWatch{client: client, subscription: subscription, stopped: make(chan struct{})}
	go watch.record(events)
	return watch, nil
}

func (watch *PeerWatch) record(events <-chan *p2p.PeerEvent) {
	defer close(watch.stopped)
	for {
		select {
		case event := <-events:
			watch.add(event)
		case err := <-watch.subscription.Err():
			// err is nil when Close unsubscribed. Only the events that reached
			// the channel are kept.
			for len(events) > 0 {
				watch.add(<-events)
			}
			if err != nil {
				watch.fail(fmt.Errorf("peer watch lost its subscription: %w", err))
			}
			return
		}
	}
}

func (watch *PeerWatch) add(event *p2p.PeerEvent) {
	if event.Type != p2p.PeerEventTypeDrop {
		return
	}
	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	watch.drops = append(watch.drops, PeerDrop{Received: time.Now(), Peer: event.Peer, RemoteAddress: event.RemoteAddress, Reason: event.Error})
}

// fail records err unless the watch has already failed.
func (watch *PeerWatch) fail(err error) {
	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	if watch.err == nil {
		watch.err = err
	}
}

// check calls the node over the watch's connection and fails if it gets no
// answer in time.
func (watch *PeerWatch) check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := watch.client.CallContext(ctx, nil, "rpc_modules"); err != nil {
		return fmt.Errorf("peer watch cannot reach the node: %w", err)
	}
	return nil
}

// Drops returns the peers dropped so far, oldest first, and an error if the
// node stopped answering.
func (watch *PeerWatch) Drops(ctx context.Context) ([]PeerDrop, error) {
	watch.mutex.Lock()
	settled := watch.closed || watch.err != nil
	watch.mutex.Unlock()

	var unreachable error
	if !settled {
		unreachable = watch.check(ctx)
	}

	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	return slices.Clone(watch.drops), cmp.Or(watch.err, unreachable)
}

// Close checks the node one last time and ends the watch. It can take up to
// 20 seconds on a stalled node.
func (watch *PeerWatch) Close() {
	watch.mutex.Lock()
	closed, failed := watch.closed, watch.err != nil
	watch.closed = true
	watch.mutex.Unlock()
	if closed {
		return
	}

	if !failed {
		if err := watch.check(context.Background()); err != nil {
			watch.fail(err)
		}
	}
	watch.subscription.Unsubscribe()
	<-watch.stopped
	watch.client.Close()
}
