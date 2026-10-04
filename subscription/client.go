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

	"github.com/tigusigalpa/bitquery-go/v2"
	"github.com/tigusigalpa/bitquery-go/v2/internal/redact"
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
	Type     EventType
	Payload  json.RawMessage
	Raw      json.RawMessage
	Delivery Delivery
	Receipt  bitquery.Receipt
}

// Delivery describes when an event frame was received. Sequence is monotonic
// across the stream and includes protocol frames that are not exposed as
// events. ReconnectGap marks the first data event after a reconnect as a
// possible delivery gap; it never means that a gap was recovered.
type Delivery struct {
	ConnectionEpoch uint64
	ReceiveSequence uint64
	ReceivedAt      time.Time
	ReconnectGap    bool
}

// GapReason identifies an observable condition that can make stream delivery
// incomplete. The SDK reports it but never attempts replay or persistence.
type GapReason string

const (
	// GapReconnect marks a successful reconnect after a connection ended.
	GapReconnect GapReason = "reconnect"
	// GapOverflowFail marks a queue overflow that stopped a fail-closed stream.
	GapOverflowFail GapReason = "overflow_fail"
	// GapReceiptOverflow marks bounded receipt retention that discarded or
	// rejected a receipt.
	GapReceiptOverflow GapReason = "receipt_overflow"
	// GapReceiptObserver marks a receipt observer that returned an error.
	GapReceiptObserver GapReason = "receipt_observer"
)

// DeliveryGap records a possible discontinuity in a Stream. It is evidence
// for caller-owned ingestion, not proof that a delivery was lost.
type DeliveryGap struct {
	Reason          GapReason
	ConnectionEpoch uint64
	ReceiveSequence uint64
	ObservedAt      time.Time
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

	dropped        atomic.Int64
	receiptDropped atomic.Int64
	gapDropped     atomic.Int64

	receiptCapacity       int
	receiptOverflowPolicy string
	receiptObserver       bitquery.ReceiptObserver
	gapCapacity           int

	receipts        []bitquery.Receipt
	gaps            []DeliveryGap
	receiveSequence uint64
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

// DroppedReceipts counts receipts discarded from the bounded retained buffer
// under OverflowDropOldest. Any non-zero value means Receipts is incomplete;
// consult Gaps for the retained discontinuity evidence.
func (s *Stream) DroppedReceipts() int64 {
	return s.receiptDropped.Load()
}

// DroppedGaps counts gap records evicted from the bounded gap buffer. A
// non-zero value means Gaps is incomplete; callers should persist gap evidence
// outside the SDK when they need a durable audit trail.
func (s *Stream) DroppedGaps() int64 {
	return s.gapDropped.Load()
}

// Receipts returns a copy of the current bounded receipt buffer. It is not an
// unbounded stream history. Use DrainReceipts in a long-lived consumer or a
// ReceiptObserver for synchronous caller-controlled backpressure.
func (s *Stream) Receipts() []bitquery.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bitquery.Receipt(nil), s.receipts...)
}

// DrainReceipts returns and clears the current bounded receipt buffer. It is
// safe to call concurrently with subscription delivery. Receipts accepted
// after the drain remain available in subsequent calls.
func (s *Stream) DrainReceipts() []bitquery.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	drained := append([]bitquery.Receipt(nil), s.receipts...)
	clear(s.receipts)
	s.receipts = s.receipts[:0]
	return drained
}

// Gaps returns a copy of the current bounded gap buffer. A reconnect is a
// possible delivery gap, not an automatic replay guarantee. DroppedGaps
// reports whether older gap records were evicted.
func (s *Stream) Gaps() []DeliveryGap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DeliveryGap(nil), s.gaps...)
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

func (s *Stream) nextDelivery(epoch uint64, receivedAt time.Time) Delivery {
	s.mu.Lock()
	s.receiveSequence++
	delivery := Delivery{
		ConnectionEpoch: epoch,
		ReceiveSequence: s.receiveSequence,
		ReceivedAt:      receivedAt.UTC(),
	}
	s.mu.Unlock()
	return delivery
}

func (s *Stream) addGap(gap DeliveryGap) {
	s.mu.Lock()
	if len(s.gaps) >= s.gapCapacity {
		copy(s.gaps, s.gaps[1:])
		s.gaps[len(s.gaps)-1] = gap
		s.gapDropped.Add(1)
		s.mu.Unlock()
		return
	}
	s.gaps = append(s.gaps, gap)
	s.mu.Unlock()
}

func (s *Stream) observeReceipt(ctx context.Context, receipt bitquery.Receipt, epoch, sequence uint64) error {
	if observer := s.receiptObserver; observer != nil {
		if err := observer(ctx, receipt); err != nil {
			s.addGap(DeliveryGap{
				Reason:          GapReceiptObserver,
				ConnectionEpoch: epoch,
				ReceiveSequence: sequence,
				ObservedAt:      time.Now().UTC(),
			})
			apiErr := bitquery.Wrap(bitquery.KindSubscription, "subscription receipt observer: "+err.Error(), err)
			apiErr.Receipts = []bitquery.Receipt{receipt}
			return fatal(apiErr)
		}
	}

	s.mu.Lock()
	if s.receiptCapacity == 0 {
		s.mu.Unlock()
		return nil
	}
	if len(s.receipts) < s.receiptCapacity {
		s.receipts = append(s.receipts, receipt)
		s.mu.Unlock()
		return nil
	}
	policy := s.receiptOverflowPolicy
	if policy == bitquery.OverflowDropOldest {
		copy(s.receipts, s.receipts[1:])
		s.receipts[len(s.receipts)-1] = receipt
		s.receiptDropped.Add(1)
		s.mu.Unlock()
		s.addGap(DeliveryGap{
			Reason:          GapReceiptOverflow,
			ConnectionEpoch: epoch,
			ReceiveSequence: sequence,
			ObservedAt:      time.Now().UTC(),
		})
		return nil
	}
	s.mu.Unlock()
	s.addGap(DeliveryGap{
		Reason:          GapReceiptOverflow,
		ConnectionEpoch: epoch,
		ReceiveSequence: sequence,
		ObservedAt:      time.Now().UTC(),
	})
	apiErr := bitquery.Wrap(bitquery.KindSubscription, "subscription receipt buffer overflow (bounded buffer full)", nil)
	apiErr.Receipts = []bitquery.Receipt{receipt}
	return fatal(apiErr)
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
	operationJSON, err := json.Marshal(op)
	if err != nil {
		return nil, bitquery.Wrap(bitquery.KindConfig, "serialize subscription operation: "+err.Error(), err)
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
	stream := &Stream{
		Events:                events,
		cancel:                cancel,
		done:                  make(chan struct{}),
		receiptCapacity:       c.cfg.SubscriptionReceiptCapacity,
		receiptOverflowPolicy: c.cfg.ReceiptOverflowPolicy,
		receiptObserver:       c.cfg.ReceiptObserver,
		gapCapacity:           max(1, c.cfg.SubscriptionReceiptCapacity),
	}

	go c.run(ctx, wsURL, operationJSON, stream, events)

	return stream, nil
}

func (c *Client) run(ctx context.Context, wsURL string, operationJSON json.RawMessage, stream *Stream, events chan Event) {
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
	var epoch uint64
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
		epoch++
		if epoch > 1 {
			stream.addGap(DeliveryGap{
				Reason:          GapReconnect,
				ConnectionEpoch: epoch,
				ObservedAt:      time.Now().UTC(),
			})
		}

		finished, acked, srvErr := c.serve(ctx, conn, operationJSON, wsURL, epoch, stream, events)

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
func (c *Client) serve(ctx context.Context, conn bitquery.WSConn, operationJSON json.RawMessage, wsURL string, epoch uint64, stream *Stream, events chan Event) (bool, bool, error) {
	init := map[string]any{"type": "connection_init", "payload": map[string]any{}}
	if err := writeJSON(ctx, conn, init, stream, operationJSON, wsURL, epoch); err != nil {
		return false, false, err
	}

	acknowledged := false
	firstDataAfterReconnect := epoch > 1
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return false, acknowledged, err
		}
		receivedAt := time.Now().UTC()
		delivery := stream.nextDelivery(epoch, receivedAt)
		receipt := bitquery.NewReceipt(
			bitquery.ReceiptSourceWebSocket,
			bitquery.ReceiptReceived,
			wsURL,
			operationJSON,
			data,
			0,
			receivedAt,
		)
		if err := stream.observeReceipt(ctx, receipt, epoch, delivery.ReceiveSequence); err != nil {
			return false, acknowledged, err
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
			sub := struct {
				ID      string          `json:"id"`
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}{
				ID:      "1",
				Type:    c.cfg.SubProtocol.SubscribeType(),
				Payload: operationJSON,
			}
			if err := writeJSON(ctx, conn, sub, stream, operationJSON, wsURL, epoch); err != nil {
				return false, acknowledged, err
			}

		case "ping":
			if err := writeJSON(ctx, conn, map[string]any{"type": "pong"}, stream, operationJSON, wsURL, epoch); err != nil {
				return false, acknowledged, err
			}

		case "ka", "pong":
			if err := c.enqueue(ctx, stream, events, Event{
				Type:     EventKeepalive,
				Raw:      append(json.RawMessage(nil), data...),
				Delivery: delivery,
				Receipt:  receipt,
			}); err != nil {
				return false, acknowledged, err
			}

		case "connection_error", "error":
			return false, acknowledged, fatal(bitquery.Wrap(bitquery.KindSubscription,
				"Bitquery WS "+msg.Type+": "+redact.String(string(msg.Payload)), nil))

		case "complete":
			if !acknowledged {
				return false, false, fatal(bitquery.Wrap(bitquery.KindSubscription, "received complete before connection_ack", nil))
			}
			if err := c.enqueue(ctx, stream, events, Event{
				Type:     EventComplete,
				Raw:      append(json.RawMessage(nil), data...),
				Delivery: delivery,
				Receipt:  receipt,
			}); err != nil {
				return false, acknowledged, err
			}
			return true, true, nil

		default:
			if msg.Type == c.cfg.SubProtocol.DataType() {
				if !acknowledged {
					return false, false, fatal(bitquery.Wrap(bitquery.KindSubscription, "received subscription data before connection_ack", nil))
				}
				delivery.ReconnectGap = firstDataAfterReconnect
				firstDataAfterReconnect = false
				if err := c.enqueue(ctx, stream, events, Event{
					Type:     EventData,
					Payload:  append(json.RawMessage(nil), msg.Payload...),
					Raw:      append(json.RawMessage(nil), data...),
					Delivery: delivery,
					Receipt:  receipt,
				}); err != nil {
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
		stream.addGap(DeliveryGap{
			Reason:          GapOverflowFail,
			ConnectionEpoch: ev.Delivery.ConnectionEpoch,
			ReceiveSequence: ev.Delivery.ReceiveSequence,
			ObservedAt:      time.Now().UTC(),
		})
		apiErr := bitquery.Wrap(bitquery.KindSubscription, "subscription event queue overflow (bounded buffer full)", nil)
		apiErr.Receipts = []bitquery.Receipt{ev.Receipt}
		return fatal(apiErr)
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

func writeJSON(ctx context.Context, conn bitquery.WSConn, v any, stream *Stream, operationJSON json.RawMessage, wsURL string, epoch uint64) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, bitquery.WSMessageText, data); err != nil {
		return err
	}
	receipt := bitquery.NewReceipt(
		bitquery.ReceiptSourceWebSocket,
		bitquery.ReceiptSent,
		wsURL,
		operationJSON,
		data,
		0,
		time.Now(),
	)
	return stream.observeReceipt(ctx, receipt, epoch, 0)
}
