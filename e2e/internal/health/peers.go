// Package health watches nodes for signs of trouble while a suite runs.
package health

import (
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
	// Time is when the watch received the drop.
	Time time.Time
	Peer qnode.ID
	// Address is the peer's remote host:port.
	Address string
	// Reason is the node's error for the disconnection.
	Reason string
}

// PeerWatch records the peers an execution node drops.
type PeerWatch struct {
	client       *rpc.Client
	subscription *rpc.ClientSubscription
	stopped      chan struct{}

	mutex sync.Mutex
	drops []PeerDrop
	err   error
}

// WatchPeers starts recording the peers dropped by the node at url, a
// WebSocket endpoint that serves the admin API. ctx bounds the dial and the
// subscription only; Close ends the watch.
func WatchPeers(ctx context.Context, url string) (*PeerWatch, error) {
	client, err := rpc.DialContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	events := make(chan *p2p.PeerEvent, 64)
	subscription, err := client.Subscribe(ctx, "admin", events, "peerEvents")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("subscribe to the peer events of %s: %w", url, err)
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
			// err is nil when Close unsubscribed.
			for len(events) > 0 {
				watch.add(<-events)
			}
			watch.mutex.Lock()
			watch.err = err
			watch.mutex.Unlock()
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
	watch.drops = append(watch.drops, PeerDrop{Time: time.Now(), Peer: event.Peer, Address: event.RemoteAddress, Reason: event.Error})
}

// Drops returns the peers dropped since the watch began, oldest first. Once
// the watch has lost its subscription it returns the drops seen until then
// with an error: after that, no drops says nothing about the node.
//
// A drop the node has just sent may not have arrived yet, and a node that
// stalls without closing the connection is noticed only after about a minute
// of failed pings.
func (watch *PeerWatch) Drops() ([]PeerDrop, error) {
	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	drops := slices.Clone(watch.drops)
	if watch.err != nil {
		return drops, fmt.Errorf("peer watch lost its subscription: %w", watch.err)
	}
	return drops, nil
}

// Close ends the watch, waiting up to 10 seconds for a node that does not
// answer the unsubscribe. Drops still returns what it recorded.
func (watch *PeerWatch) Close() {
	watch.subscription.Unsubscribe()
	<-watch.stopped
	watch.client.Close()
}
