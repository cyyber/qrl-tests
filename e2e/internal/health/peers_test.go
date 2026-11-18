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

// fakeAdmin is a node's admin API, as far as its peer events, and the server
// that serves it.
type fakeAdmin struct {
	events chan *p2p.PeerEvent
	api    *rpc.Server
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

// startFakeNode serves an API over WebSocket and returns its URL. Stopping
// the API closes its connections.
func startFakeNode(t *testing.T) (url string, api *rpc.Server) {
	t.Helper()
	api = rpc.NewServer()
	t.Cleanup(api.Stop)
	server := httptest.NewServer(api.WebsocketHandler([]string{"*"}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), api
}

// startWatch watches a fake node with the admin API.
func startWatch(t *testing.T) (*fakeAdmin, *PeerWatch) {
	t.Helper()
	url, api := startFakeNode(t)
	admin := &fakeAdmin{events: make(chan *p2p.PeerEvent), api: api}
	require.NoError(t, api.RegisterName("admin", admin))
	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)
	return admin, watch
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

// dropped is the event of a node dropping peer, at peerAddress.
func dropped(peer qnode.ID, reason string) *p2p.PeerEvent {
	return &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: peer, RemoteAddress: peerAddress, Error: reason}
}

const peerAddress = "172.16.0.12:30303"

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
	admin, watch := startWatch(t)
	defer watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Empty(t, drops)

	first, second := qnode.ID{1}, qnode.ID{2}
	began := time.Now()
	send(t, admin, &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: first})
	send(t, admin, dropped(first, "message too long: 11 > 10"))
	send(t, admin, dropped(second, "useless peer"))

	drops = awaitDrops(t, watch, 2)
	for index := range drops {
		require.WithinRange(t, drops[index].Received, began, time.Now())
		drops[index].Received = time.Time{}
	}
	require.Equal(t, []PeerDrop{
		{Peer: first, RemoteAddress: peerAddress, Reason: "message too long: 11 > 10"},
		{Peer: second, RemoteAddress: peerAddress, Reason: "useless peer"},
	}, drops)

	again, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.False(t, again[0].Received.IsZero(), "the watch keeps its own copy")
}

func TestDropsFailsWhileTheNodeDoesNotAnswer(t *testing.T) {
	_, watch := startWatch(t)
	defer watch.Close()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := watch.Drops(cancelled)
	require.ErrorContains(t, err, "peer watch cannot reach the node")

	_, err = watch.Drops(t.Context())
	require.NoError(t, err, "a node that answers again is watched again")
}

func TestCloseKeepsTheDrops(t *testing.T) {
	admin, watch := startWatch(t)

	send(t, admin, dropped(qnode.ID{1}, "useless peer"))
	awaitDrops(t, watch, 1)

	watch.Close()
	// A second Close does nothing.
	watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Len(t, drops, 1)
}

func TestDropsAfterTheSubscriptionIsLost(t *testing.T) {
	admin, watch := startWatch(t)

	send(t, admin, dropped(qnode.ID{1}, "useless peer"))
	awaitDrops(t, watch, 1)

	admin.api.Stop()
	select {
	case <-watch.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not end")
	}

	drops, err := watch.Drops(t.Context())
	require.ErrorContains(t, err, "peer watch lost its subscription")
	require.Len(t, drops, 1, "the drops recorded before the loss")

	watch.Close()
	drops, err = watch.Drops(t.Context())
	require.ErrorContains(t, err, "peer watch lost its subscription", "Close keeps the error")
	require.Len(t, drops, 1)
}

func TestWatchPeersNeedsTheAdminAPI(t *testing.T) {
	url, _ := startFakeNode(t)
	_, err := WatchPeers(t.Context(), url)
	require.ErrorContains(t, err, "subscribe to the peer events")
}
