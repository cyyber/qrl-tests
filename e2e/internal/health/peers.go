// Package health checks that a suite's load leaves the network healthy.
package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
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
	ended        chan struct{}

	mutex sync.Mutex
	drops []PeerDrop
	err   error
}

const (
	startTimeout = 10 * time.Second
	checkTimeout = 10 * time.Second
)

// WatchPeers records the peers dropped by the node at wsURL until Close.
func WatchPeers(ctx context.Context, wsURL string) (*PeerWatch, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	client, err := rpc.DialOptions(ctx, wsURL, rpc.WithWebsocketDialer(dialOnce()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	events := make(chan *p2p.PeerEvent, 64)
	subscription, err := client.Subscribe(ctx, "admin", events, "peerEvents")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("subscribe to admin_peerEvents on %s: %w", wsURL, err)
	}

	watch := &PeerWatch{client: client, subscription: subscription, ended: make(chan struct{})}
	go watch.record(events)
	return watch, nil
}

var errRedial = errors.New("the peer watch does not reconnect")

// dialOnce is a WebSocket dialer that connects once and then refuses, since
// the subscription lives on the first connection only.
func dialOnce() websocket.Dialer {
	var dialed atomic.Bool
	return websocket.Dialer{
		Proxy: http.ProxyFromEnvironment,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if dialed.Swap(true) {
				return nil, errRedial
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
}

func (watch *PeerWatch) record(events <-chan *p2p.PeerEvent) {
	defer close(watch.ended)
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
	watch.drops = append(watch.drops, PeerDrop{
		Received:      time.Now(),
		Peer:          event.Peer,
		RemoteAddress: event.RemoteAddress,
		Reason:        event.Error,
	})
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
// answer in time. A watch that has ended keeps the error it ended with.
func (watch *PeerWatch) check(ctx context.Context) error {
	select {
	case <-watch.ended:
		return nil
	default:
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := watch.client.CallContext(ctx, nil, "rpc_modules"); err != nil {
		return fmt.Errorf("peer watch cannot reach the node: %w", err)
	}
	return nil
}

// Drops returns the peers dropped so far, oldest first, and an error if the
// watch lost its subscription or the node does not answer.
func (watch *PeerWatch) Drops(ctx context.Context) ([]PeerDrop, error) {
	unreachable := watch.check(ctx)

	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	return slices.Clone(watch.drops), cmp.Or(watch.err, unreachable)
}

// Close checks the node one last time and ends the watch. It can take up to
// 20 seconds on a stalled node.
func (watch *PeerWatch) Close() {
	if err := watch.check(context.Background()); err != nil {
		watch.fail(err)
	}

	watch.subscription.Unsubscribe()
	<-watch.ended
	watch.client.Close()
}
