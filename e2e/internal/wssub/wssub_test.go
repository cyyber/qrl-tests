package wssub

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// serve runs a WebSocket server that hands each connection to handle, and
// returns its URL.
func serve(t *testing.T, handle func(*websocket.Conn)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		handle(ws)
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func readRequest(t *testing.T, ws *websocket.Conn) (id uint64, params []any) {
	t.Helper()
	var request struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	require.NoError(t, ws.ReadJSON(&request))
	require.Equal(t, "qrl_subscribe", request.Method)
	return request.ID, request.Params
}

func notify(t *testing.T, ws *websocket.Conn, subscription string, result any) {
	t.Helper()
	require.NoError(t, ws.WriteJSON(map[string]any{
		"jsonrpc": "2.0", "method": "qrl_subscription",
		"params": map[string]any{"subscription": subscription, "result": result},
	}))
}

func TestSubscribeAndRead(t *testing.T) {
	url := serve(t, func(ws *websocket.Conn) {
		id, params := readRequest(t, ws)
		require.Equal(t, []any{"newHeads"}, params)
		require.NoError(t, ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": "0xa"}))
		notify(t, ws, "0xa", map[string]string{"number": "0x1"})

		id, params = readRequest(t, ws)
		require.Equal(t, []any{"logs", map[string]any{}}, params)
		// A notification of the first subscription arrives before the reply.
		notify(t, ws, "0xa", map[string]string{"number": "0x2"})
		require.NoError(t, ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": "0xb"}))
		notify(t, ws, "0xb", "log")
	})
	conn, err := Dial(t.Context(), url)
	require.NoError(t, err)
	defer conn.Close()

	heads, err := conn.Subscribe(t.Context(), "newHeads")
	require.NoError(t, err)
	require.Equal(t, "0xa", heads)
	logs, err := conn.Subscribe(t.Context(), "logs", map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "0xb", logs)

	var received []string
	for range 3 {
		next, err := conn.Next(time.Now().Add(5 * time.Second))
		require.NoError(t, err)
		require.Positive(t, next.Size)
		received = append(received, fmt.Sprintf("%s %s", next.Subscription, next.Result))
	}
	require.Equal(t, []string{`0xa {"number":"0x1"}`, `0xa {"number":"0x2"}`, `0xb "log"`}, received)

	// The server has closed the connection.
	_, err = conn.Next(time.Now().Add(5 * time.Second))
	require.Error(t, err)
	require.False(t, TimedOut(err), "%v", err)
}

func TestNextTimesOut(t *testing.T) {
	done := make(chan struct{})
	url := serve(t, func(ws *websocket.Conn) {
		id, _ := readRequest(t, ws)
		require.NoError(t, ws.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": "0xa"}))
		<-done
	})
	defer close(done)
	conn, err := Dial(t.Context(), url)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Subscribe(t.Context(), "newHeads")
	require.NoError(t, err)

	_, err = conn.Next(time.Now().Add(50 * time.Millisecond))
	require.True(t, TimedOut(err), "%v", err)
}

func TestSubscribeRefused(t *testing.T) {
	url := serve(t, func(ws *websocket.Conn) {
		id, _ := readRequest(t, ws)
		require.NoError(t, ws.WriteJSON(map[string]any{
			"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "no such subscription"},
		}))
		_, _, _ = ws.ReadMessage()
	})
	conn, err := Dial(t.Context(), url)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Subscribe(t.Context(), "unknown")
	require.ErrorContains(t, err, "-32601 no such subscription")
}
