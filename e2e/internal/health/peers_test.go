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

// startWatch watches a fake node, which the test stops through api.
func startWatch(t *testing.T) (admin *fakeAdmin, watch *PeerWatch, api *rpc.Server) {
	t.Helper()
	admin = &fakeAdmin{events: make(chan *p2p.PeerEvent)}
	url, api := startFakeNode(t, admin)
	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)
	return admin, watch, api
}

// send has the fake node emit event, failing the test if nothing takes it.
func send(t *testing.T, admin *fakeAdmin, event *p2p.PeerEvent) {
	t.Helper()
	select {
	case admin.events <- event:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake node took no event")
	}
}

func awaitDrops(t *testing.T, watch *PeerWatch, count int) []PeerDrop {
	t.Helper()
	var drops []PeerDrop
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		drops, err = watch.Drops(t.Context())
		assert.NoError(c, err)
		assert.Len(c, drops, count)
	}, 5*time.Second, 10*time.Millisecond)
	return drops
}

func TestWatchPeersRecordsDrops(t *testing.T) {
	admin, watch, _ := startWatch(t)
	defer watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Empty(t, drops)

	first, second := qnode.ID{1}, qnode.ID{2}
	began := time.Now()
	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: first})
	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: first, Error: "message too long: 11 > 10", RemoteAddress: "172.16.0.12:30303"})
	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: second})
	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: second, Error: "useless peer"})

	drops = awaitDrops(t, watch, 2)
	require.WithinRange(t, drops[0].Time, began, time.Now())
	require.False(t, drops[1].Time.Before(drops[0].Time), "oldest first")
	for index := range drops {
		drops[index].Time = time.Time{}
	}
	require.Equal(t, []PeerDrop{
		{Peer: first, Address: "172.16.0.12:30303", Reason: "message too long: 11 > 10"},
		{Peer: second, Reason: "useless peer"},
	}, drops)

	again, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.False(t, again[0].Time.IsZero(), "the watch keeps its own copy")
}

func TestDropsFailsWhileTheNodeDoesNotAnswer(t *testing.T) {
	_, watch, _ := startWatch(t)
	defer watch.Close()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := watch.Drops(cancelled)
	require.ErrorContains(t, err, "peer watch cannot reach the node")

	_, err = watch.Drops(t.Context())
	require.NoError(t, err, "a node that answers again is watched again")
}

func TestCloseKeepsTheDrops(t *testing.T) {
	admin, watch, _ := startWatch(t)

	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: qnode.ID{1}, Error: "useless peer"})
	awaitDrops(t, watch, 1)

	watch.Close()
	watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Len(t, drops, 1)
}

func TestDropsAfterTheSubscriptionIsLost(t *testing.T) {
	admin, watch, api := startWatch(t)

	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: qnode.ID{1}, Error: "useless peer"})
	awaitDrops(t, watch, 1)

	api.Stop()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Until the loss arrives, the check of the stopped node times out.
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		drops, err := watch.Drops(ctx)
		assert.ErrorContains(c, err, "peer watch lost its subscription")
		assert.Len(c, drops, 1, "the drops recorded before the loss")
	}, 5*time.Second, 10*time.Millisecond)

	watch.Close()
	drops, err := watch.Drops(t.Context())
	require.ErrorContains(t, err, "peer watch lost its subscription", "Close keeps the error")
	require.Len(t, drops, 1)
}

func TestWatchPeersNeedsTheAdminAPI(t *testing.T) {
	url, _ := startFakeNode(t, nil)
	_, err := WatchPeers(t.Context(), url)
	require.ErrorContains(t, err, "subscribe to the peer events")
}
