// Package wssub subscribes to a node's notifications over a WebSocket whose
// reading the caller controls: a subscriber that stops reading leaves the
// node's writes to back up, as a slow client's do.
package wssub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

// Conn is a WebSocket connection to a node's JSON-RPC API. It reads only when
// Subscribe or Next is called.
type Conn struct {
	ws       *websocket.Conn
	requests uint64
	// queued are the notifications read while Subscribe waited for its reply.
	queued []Notification
}

// Notification is one message of a subscription.
type Notification struct {
	Subscription string
	Result       json.RawMessage
	// Size is the length of the message that carried it.
	Size int
}

type message struct {
	ID     *uint64         `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Method string `json:"method"`
	Params struct {
		Subscription string          `json:"subscription"`
		Result       json.RawMessage `json:"result"`
	} `json:"params"`
}

// Dial opens a connection without a limit on the size of a message it reads.
func Dial(ctx context.Context, url string) (*Conn, error) {
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	return &Conn{ws: ws}, nil
}

// Subscribe starts a subscription, such as to "newHeads", and returns its ID.
func (conn *Conn) Subscribe(ctx context.Context, params ...any) (string, error) {
	conn.requests++
	id := conn.requests
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "qrl_subscribe", "params": params})
	if err != nil {
		return "", err
	}
	deadline, _ := ctx.Deadline()
	_ = conn.ws.SetWriteDeadline(deadline)
	if err := conn.ws.WriteMessage(websocket.TextMessage, request); err != nil {
		return "", fmt.Errorf("subscribe to %v: %w", params, err)
	}
	for {
		reply, size, err := conn.read(deadline)
		if err != nil {
			return "", fmt.Errorf("read the reply to subscribing to %v: %w", params, err)
		}
		switch {
		case reply.Method != "":
			conn.queued = append(conn.queued, notification(reply, size))
		case reply.ID == nil || *reply.ID != id:
			return "", fmt.Errorf("a reply to request %v while subscribing to %v", reply.ID, params)
		case reply.Error != nil:
			return "", fmt.Errorf("subscribe to %v: %d %s", params, reply.Error.Code, reply.Error.Message)
		default:
			var subscription string
			if err := json.Unmarshal(reply.Result, &subscription); err != nil {
				return "", fmt.Errorf("subscription ID of %v: %w", params, err)
			}
			return subscription, nil
		}
	}
}

// Next returns the next notification, waiting for it until deadline, or
// without end for a zero deadline. After a read has timed out, the
// connection cannot be read again.
func (conn *Conn) Next(deadline time.Time) (Notification, error) {
	if len(conn.queued) > 0 {
		next := conn.queued[0]
		conn.queued = conn.queued[1:]
		return next, nil
	}
	for {
		received, size, err := conn.read(deadline)
		if err != nil {
			return Notification{}, err
		}
		if received.Method != "" {
			return notification(received, size), nil
		}
	}
}

func (conn *Conn) read(deadline time.Time) (message, int, error) {
	_ = conn.ws.SetReadDeadline(deadline)
	_, data, err := conn.ws.ReadMessage()
	if err != nil {
		return message{}, 0, err
	}
	var received message
	if err := json.Unmarshal(data, &received); err != nil {
		return message{}, 0, fmt.Errorf("decode a message of %d bytes: %w", len(data), err)
	}
	return received, len(data), nil
}

func notification(received message, size int) Notification {
	return Notification{Subscription: received.Params.Subscription, Result: received.Params.Result, Size: size}
}

// Close closes the connection.
func (conn *Conn) Close() error {
	return conn.ws.Close()
}

// TimedOut reports whether err is a read that ran out of time, rather than a
// connection the node ended.
func TimedOut(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
