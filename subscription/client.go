// Package subscription provides the opt-in Bitquery V2 WebSocket
// subscription client. It is a separate client by design — WebSocket
// support is never automatic.
//
// Lifecycle: dial the wss endpoint with the OAuth token ONLY in the
// "?token=" URL parameter → connection_init → connection_ack →
// subscribe → next/data frames → complete or socket close. Per Bitquery
// docs the socket cannot send "close" messages — closing it is the only
// way to end the stream.
//
// Delivery is at-least-once and unordered across block portions —
// deduplicate downstream. Reconnects use bounded exponential backoff.
//
// https://docs.bitquery.io/docs/subscriptions/websockets/
// https://docs.bitquery.io/docs/authorization/websocket/
package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tigusigalpa/bitquery-go"
	"github.com/tigusigalpa/bitquery-go/internal/redact"
)

// EventType classifies stream events.
type EventType int

const (
	// EventData carries a GraphQL payload (next/data frame).
	EventData EventType = iota + 1
	// EventKeepalive is a ka/pong frame.
	EventKeepalive
	// EventComplete marks a clean server-side completion.
	EventComplete
)

// Event is one subscription event delivered on Stream.Events.
type Event struct {
	Type    EventType
	Payload json.RawMessage
	Raw     json.RawMessage
}

// Stream is a running subscription. Close() terminates the socket — the
// only way to end a Bitquery stream. Events is closed when the stream
// ends; buffered events remain readable after close.
type Stream struct {
	Events <-chan Event

	cancel context.CancelFunc
	done   chan struct{}

	mu   sync.Mutex
	conn bitquery.WSConn
	err  error

	dropped atomic.Int64
}

// Close terminates the subscription. Idempotent.
func (s *Stream) Close() error {
	s.cancel()
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close(1000, "client close")
	}
	s.mu.Unlock()
	<-s.done
	return nil
}

// Wait blocks until the stream terminates (complete, error, Close or
// ctx cancellation). Buffered events remain readable afterwards.
func (s *Stream) Wait() { <-s.done }

// Err returns the terminal error, if any (nil for clean complete/close).
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Dropped counts events discarded by the bounded-buffer overflow policy
// (drop_oldest). Use it for at-least-once monitoring.
func (s *Stream) Dropped() int64 {
	return s.dropped.Load()
}

func (s *Stream) setConn(c bitquery.WSConn) {
	s.mu.Lock()
	s.conn = c
	s.mu.Unlock()
}

func (s *Stream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// Client is the opt-in V2 WebSocket subscription client.
type Client struct {
	cfg *bitquery.Config
}

// New builds a subscription client for V2. The token provider is
// required — the token travels in the `?token=` URL parameter only.
func New(provider bitquery.TokenProvider, opts ...bitquery.Option) (*Client, error) {
	opts = append([]bitquery.Option{bitquery.WithTokenProvider(provider)}, opts...)
	cfg, err := bitquery.NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg}, nil
}

// Endpoint returns the resolved WSS endpoint (no token — safe to log).
func (c *Client) Endpoint() (string, error) {
	return bitquery.WebSocketURL(c.cfg.Region, c.cfg.BaseURL, c.cfg.WebSocketURL)
}

// Subscribe starts a subscription and returns immediately. Events arrive
// on stream.Events until complete, Close(), ctx cancellation or a fatal
// error (then Err() reports it).
func (c *Client) Subscribe(ctx context.Context, op bitquery.Operation) (*Stream, error) {
	if ctx == nil {
		return nil, bitquery.Wrap(bitquery.KindConfig, "context cannot be nil", nil)
	}
	if strings.TrimSpace(op.Query) == "" {
		return nil, bitquery.Wrap(bitquery.KindConfig, "subscription document cannot be empty", nil)
	}
	base, err := c.Endpoint()
	if err != nil {
		return nil, err
	}
	token, err := c.cfg.TokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(base)
	if err != nil {
		return nil, bitquery.Wrap(bitquery.KindConfig, "parse WebSocket endpoint: "+err.Error(), err)
	}
	query := u.Query()
	query.Set("token", token)
	u.RawQuery = query.Encode()
	wsURL := u.String()

	ctx, cancel := context.WithCancel(ctx)
	events := make(chan Event, max(1, c.cfg.SubscriptionQueueCapacity))
	stream := &Stream{Events: events, cancel: cancel, done: make(chan struct{})}

	go c.run(ctx, wsURL, op, stream, events)

	return stream, nil
}

func (c *Client) run(ctx context.Context, wsURL string, op bitquery.Operation, stream *Stream, events chan Event) {
	defer close(events)
	defer close(stream.done)
	defer stream.cancel()

	policy := c.cfg.Retry
	if policy == nil {
		policy = bitquery.DefaultRetryPolicy()
	}
	sleep := policy.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}

	dialer := c.cfg.Dialer
	if dialer == nil {
		dialer = CoderDialer
	}

	log := c.cfg.Logger
	if log == nil {
		log = bitquery.NopLogger{}
	}

	attempts := 0
	for {
		if ctx.Err() != nil {
			return // cancelled — clean exit, no error surfaced
		}

		conn, err := dialer(ctx, wsURL, []string{string(c.cfg.SubProtocol)})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if attempts >= c.cfg.SubscriptionMaxReconnects {
				stream.setErr(bitquery.Wrap(bitquery.KindSubscription,
					"websocket dial failed after retries: "+redact.String(err.Error()), err))
				return
			}
			attempts++
			if serr := sleep(ctx, policy.Delay(attempts, 0)); serr != nil {
				return
			}
			continue
		}

		stream.setConn(conn)

		finished, acked, srvErr := c.serve(ctx, conn, op, stream, events)

		stream.setConn(nil)
		_ = conn.Close(1000, "reconnect")

		if ctx.Err() != nil {
			return
		}
		if finished {
			return // clean complete
		}
		if errors.Is(srvErr, errFatal) {
			// Client-side terminal conditions (queue overflow with the
			// "fail" policy, connection_error frames) must not reconnect.
			stream.setErr(srvErr)
			return
		}
		if acked {
			attempts = 0 // connection was healthy — reset consecutive failures
			if serr := sleep(ctx, policy.Delay(1, 0)); serr != nil {
				return
			}
			continue
		}

		if attempts >= c.cfg.SubscriptionMaxReconnects {
			if srvErr == nil {
				srvErr = errors.New("connection dropped")
			}
			stream.setErr(bitquery.Wrap(bitquery.KindSubscription,
				"reconnect attempts exhausted: "+redact.String(srvErr.Error()), srvErr))
			return
		}
		attempts++

		log.Info("bitquery ws reconnecting", "attempt", attempts)
		if serr := sleep(ctx, policy.Delay(attempts, 0)); serr != nil {
			return
		}
	}
}

// serve runs one connection: handshake-init, then the frame loop.
// Returns (finished, acknowledged, error).
func (c *Client) serve(ctx context.Context, conn bitquery.WSConn, op bitquery.Operation, stream *Stream, events chan Event) (bool, bool, error) {
	init := map[string]any{"type": "connection_init", "payload": map[string]any{}}
	if err := writeJSON(ctx, conn, init); err != nil {
		return false, false, err
	}

	acknowledged := false
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return false, false, err
		}

		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // unparseable frame — ignore
		}

		switch msg.Type {
		case "connection_ack":
			if acknowledged {
				return false, true, fatal(bitquery.Wrap(bitquery.KindSubscription, "received duplicate connection_ack", nil))
			}
			acknowledged = true
			sub := map[string]any{
				"id":      "1",
				"type":    c.cfg.SubProtocol.SubscribeType(),
				"payload": op,
			}
			if err := writeJSON(ctx, conn, sub); err != nil {
				return false, acknowledged, err
			}

		case "ping":
			if err := writeJSON(ctx, conn, map[string]any{"type": "pong"}); err != nil {
				return false, acknowledged, err
			}

		case "ka", "pong":
			if err := c.enqueue(ctx, stream, events, Event{Type: EventKeepalive, Raw: data}); err != nil {
				return false, acknowledged, err
			}

		case "connection_error", "error":
			return false, acknowledged, fatal(bitquery.Wrap(bitquery.KindSubscription,
				"Bitquery WS "+msg.Type+": "+redact.String(string(msg.Payload)), nil))

		case "complete":
			if !acknowledged {
				return false, false, fatal(bitquery.Wrap(bitquery.KindSubscription, "received complete before connection_ack", nil))
			}
			_ = c.enqueue(ctx, stream, events, Event{Type: EventComplete, Raw: data})
			return true, true, nil

		default:
			if msg.Type == c.cfg.SubProtocol.DataType() {
				if !acknowledged {
					return false, false, fatal(bitquery.Wrap(bitquery.KindSubscription, "received subscription data before connection_ack", nil))
				}
				if err := c.enqueue(ctx, stream, events, Event{Type: EventData, Payload: msg.Payload, Raw: data}); err != nil {
					return false, acknowledged, err
				}
			}
		}
	}
}

// enqueue writes to the bounded event channel applying the overflow policy.
func (c *Client) enqueue(ctx context.Context, stream *Stream, events chan Event, ev Event) error {
	select {
	case events <- ev:
		return nil
	default:
	}

	switch c.cfg.OverflowPolicy {
	case bitquery.OverflowFail:
		return fatal(&bitquery.Error{Kind: bitquery.KindSubscription, Message: "subscription event queue overflow (bounded buffer full)"})
	default: // drop_oldest
		select {
		case <-events:
			stream.dropped.Add(1)
		default:
		}
		select {
		case events <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
}

// errFatal marks stream errors that must terminate the subscription
// without reconnecting (queue overflow under the "fail" policy, server
// connection_error frames).
var errFatal = errors.New("subscription: fatal")

func fatal(err error) error {
	return fmt.Errorf("%w: %w", errFatal, err)
}

func writeJSON(ctx context.Context, conn bitquery.WSConn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, bitquery.WSMessageText, data)
}
