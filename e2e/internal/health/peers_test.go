package health

import (
	"context"
	"net"
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

// fakeAdmin is a node's admin API with only its peer events, and its server.
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

// emit has the node send event to the watch, failing the test if it isn't
// taken within 5 seconds.
func (admin *fakeAdmin) emit(t *testing.T, event *p2p.PeerEvent) {
	t.Helper()
	select {
	case admin.events <- event:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake node took no event")
	}
}

// startFakeNode serves an API over WebSocket. Stopping it closes the connections.
func startFakeNode(t *testing.T) (url string, api *rpc.Server) {
	t.Helper()
	api = rpc.NewServer()
	t.Cleanup(api.Stop)
	server := httptest.NewServer(api.WebsocketHandler([]string{"*"}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), api
}

// startPeerWatch watches the peers of a fake node with the admin API.
func startPeerWatch(t *testing.T) (*fakeAdmin, *PeerWatch) {
	t.Helper()
	url, api := startFakeNode(t)
	admin := &fakeAdmin{events: make(chan *p2p.PeerEvent), api: api}
	require.NoError(t, api.RegisterName("admin", admin))
	watch, err := WatchPeers(t.Context(), url)
	require.NoError(t, err)
	return admin, watch
}

const peerAddress = "172.16.0.12:30303"

func dropped(peer qnode.ID, reason string) *p2p.PeerEvent {
	return &p2p.PeerEvent{Type: p2p.PeerEventTypeDrop, Peer: peer, RemoteAddress: peerAddress, Error: reason}
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

func TestWatchPeers(t *testing.T) {
	admin, watch := startPeerWatch(t)
	defer watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Empty(t, drops)

	first, second := qnode.ID{1}, qnode.ID{2}
	began := time.Now()
	admin.emit(t, &p2p.PeerEvent{Type: p2p.PeerEventTypeAdd, Peer: first})
	admin.emit(t, dropped(first, "message too long: 11 > 10"))
	admin.emit(t, dropped(second, "useless peer"))

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

func TestDropsFailedCheck(t *testing.T) {
	admin, watch := startPeerWatch(t)
	defer watch.Close()

	admin.emit(t, dropped(qnode.ID{1}, "useless peer"))
	awaitDrops(t, watch, 1)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	drops, err := watch.Drops(cancelled)
	require.ErrorContains(t, err, "peer watch cannot reach the node")
	require.Len(t, drops, 1, "the drops come with the error")

	_, err = watch.Drops(t.Context())
	require.NoError(t, err, "a failed check is not kept")
}

func TestDropsAfterClose(t *testing.T) {
	admin, watch := startPeerWatch(t)

	admin.emit(t, dropped(qnode.ID{1}, "useless peer"))
	awaitDrops(t, watch, 1)

	watch.Close()
	// A second Close does nothing.
	watch.Close()

	drops, err := watch.Drops(t.Context())
	require.NoError(t, err)
	require.Len(t, drops, 1)
}

func TestLostSubscription(t *testing.T) {
	admin, watch := startPeerWatch(t)

	admin.emit(t, dropped(qnode.ID{1}, "useless peer"))
	awaitDrops(t, watch, 1)

	admin.api.Stop()
	select {
	case <-watch.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not end")
	}

	err := watch.client.CallContext(t.Context(), nil, "rpc_modules")
	require.ErrorIs(t, err, errRedial, "the client does not reconnect")

	drops, err := watch.Drops(t.Context())
	require.ErrorContains(t, err, "peer watch lost its subscription")
	require.Len(t, drops, 1, "the drops recorded before the loss")

	watch.Close()
	drops, err = watch.Drops(t.Context())
	require.ErrorContains(t, err, "peer watch lost its subscription", "Close keeps the error")
	require.Len(t, drops, 1)
}

func TestWatchPeersNoAdmin(t *testing.T) {
	url, _ := startFakeNode(t)
	_, err := WatchPeers(t.Context(), url)
	require.ErrorContains(t, err, "subscribe to admin_peerEvents")
}

func TestDialOnce(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	address, dialer := listener.Addr().String(), dialOnce()

	conn, err := dialer.NetDialContext(t.Context(), "tcp", address)
	require.NoError(t, err)
	conn.Close()

	_, err = dialer.NetDialContext(t.Context(), "tcp", address)
	require.ErrorIs(t, err, errRedial)
}
