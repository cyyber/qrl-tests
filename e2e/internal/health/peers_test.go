package health

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theQRL/go-qrl/p2p"
	"github.com/theQRL/go-qrl/p2p/qnode"
	"github.com/theQRL/go-qrl/rpc"
)

// fakeAdmin is a node's admin API, as far as its peer events.
type fakeAdmin struct {
	events chan *p2p.PeerEvent
}

func (admin *fakeAdmin) PeerEvents(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}
	subscription := notifier.CreateSubscription()
	go func() {
		for {
			select {
			case event := <-admin.events:
				_ = notifier.Notify(subscription.ID, event)
			case <-subscription.Err():
				return
			}
		}
	}()
	return subscription, nil
}

// startFakeNode serves the API over WebSocket and returns its URL. Stopping
// the API closes its connections.
func startFakeNode(t *testing.T, admin *fakeAdmin) (url string, api *rpc.Server) {
	t.Helper()
	api = rpc.NewServer()
	t.Cleanup(api.Stop)
	if admin != nil {
		require.NoError(t, api.RegisterName("admin", admin))
	}
	server := httptest.NewServer(api.WebsocketHandler([]string{"*"}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), api
}

func awaitDrops(t *testing.T, watch *PeerWatch, count int) []PeerDrop {
	t.Helper()
	var drops []PeerDrop
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		drops, err = watch.Drops()
		assert.NoError(c, err)
		assert.Len(c, drops, count)
	}, 5*time.Second, 10*time.Millisecond)
	return drops
}

func TestWatchPeersRecordsDrops(t *testing.T) {
	admin := &fakeAdmin{events: make(chan *p2p.PeerEvent)}
	url, _ := startFakeNode(t, admin)

	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)
	defer watch.Close()

	drops, err := watch.Drops()
	require.NoError(t, err)
	require.Empty(t, drops)

	first, second := qnode.ID{1}, qnode.ID{2}
	began := time.Now()
	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: first}
	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: first, Error: "message too long: 11 > 10", RemoteAddress: "172.16.0.12:30303"}
	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: second}
	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: second, Error: "useless peer"}

	drops = awaitDrops(t, watch, 2)
	require.Equal(t, PeerDrop{Time: drops[0].Time, Peer: first, Address: "172.16.0.12:30303", Reason: "message too long: 11 > 10"}, drops[0])
	require.Equal(t, second, drops[1].Peer)
	require.Equal(t, "useless peer", drops[1].Reason)
	require.WithinRange(t, drops[0].Time, began, time.Now())
	require.False(t, drops[1].Time.Before(drops[0].Time), "oldest first")

	drops[0].Reason = "changed"
	again, err := watch.Drops()
	require.NoError(t, err)
	require.Equal(t, "message too long: 11 > 10", again[0].Reason, "the watch keeps its own copy")
}

func TestCloseKeepsTheDrops(t *testing.T) {
	admin := &fakeAdmin{events: make(chan *p2p.PeerEvent)}
	url, _ := startFakeNode(t, admin)

	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)

	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: qnode.ID{1}, Error: "useless peer"}
	awaitDrops(t, watch, 1)

	watch.Close()

	drops, err := watch.Drops()
	require.NoError(t, err)
	require.Len(t, drops, 1)
}

func TestDropsFailsOnceTheSubscriptionIsLost(t *testing.T) {
	admin := &fakeAdmin{events: make(chan *p2p.PeerEvent)}
	url, api := startFakeNode(t, admin)

	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)
	defer watch.Close()

	admin.events <- &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: qnode.ID{1}, Error: "useless peer"}
	awaitDrops(t, watch, 1)

	api.Stop()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		drops, err := watch.Drops()
		assert.ErrorContains(c, err, "peer watch lost its subscription")
		assert.Len(c, drops, 1, "the drops seen before the loss")
	}, 5*time.Second, 10*time.Millisecond)
}

func TestWatchPeersNeedsTheAdminAPI(t *testing.T) {
	url, _ := startFakeNode(t, nil)
	_, err := WatchPeers(t.Context(), url)
	require.ErrorContains(t, err, "subscribe to the peer events")
}
