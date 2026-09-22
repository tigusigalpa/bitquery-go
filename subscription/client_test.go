package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tigusigalpa/bitquery-go"
)

// ---- fake connection ------------------------------------------------

type readResult struct {
	data []byte
	err  error
}

type fakeConn struct {
	reads chan readResult

	mu       sync.Mutex
	writes   [][]byte
	closed   bool
	closeErr chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{reads: make(chan readResult, 128), closeErr: make(chan struct{}, 1)}
}

func (c *fakeConn) Read(ctx context.Context) (bitquery.WSMessageType, []byte, error) {
	select {
	case r, ok := <-c.reads:
		if !ok {
			return 0, nil, io.EOF // socket closed by "server"
		}
		return bitquery.WSMessageText, r.data, r.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (c *fakeConn) Write(_ context.Context, _ bitquery.WSMessageType, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("write on closed conn")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	c.writes = append(c.writes, cp)
	return nil
}

func (c *fakeConn) Close(uint32, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.reads)
	}
	return nil
}

func (c *fakeConn) push(v any) {
	data, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.reads <- readResult{data: data}
}

func (c *fakeConn) pushErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.reads <- readResult{err: err}
}

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// waitWrite blocks until at least n writes were recorded or the timeout
// expires (nil result). Safe to call from server-driver goroutines —
// assertions stay in the test goroutine.
func (c *fakeConn) waitWrite(n int) [][]byte {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.writes) >= n {
			out := make([][]byte, len(c.writes))
			copy(out, c.writes)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	return nil
}

// ---- scripted dialer ------------------------------------------------

type dialRec struct {
	urls  []string
	conns []*fakeConn
	err   error // if set, dial always fails
	serve func(*fakeConn)
	mu    sync.Mutex
}

func (d *dialRec) dialer() bitquery.Dialer {
	return func(ctx context.Context, url string, _ []string) (bitquery.WSConn, error) {
		d.mu.Lock()
		d.urls = append(d.urls, url)
		err := d.err
		d.mu.Unlock()
		if err != nil {
			return nil, err
		}
		conn := newFakeConn()
		d.mu.Lock()
		d.conns = append(d.conns, conn)
		serve := d.serve
		d.mu.Unlock()
		if serve != nil {
			go serve(conn)
		}
		return conn, nil
	}
}

func (d *dialRec) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.urls)
}

func (d *dialRec) conn(t *testing.T, i int) *fakeConn {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		if len(d.conns) > i {
			c := d.conns[i]
			d.mu.Unlock()
			return c
		}
		d.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("conn %d never dialed", i)
	return nil
}

// ---- helpers --------------------------------------------------------

func newTestClient(t *testing.T, d *dialRec, extra ...bitquery.Option) *Client {
	t.Helper()
	policy := bitquery.DefaultRetryPolicy()
	policy.Sleep = func(context.Context, time.Duration) error { return nil }
	policy.BaseDelay = time.Millisecond
	policy.MaxDelay = 5 * time.Millisecond
	policy.Jitter = 0

	opts := append([]bitquery.Option{
		bitquery.WithWebSocketURL("wss://ws.test/graphql"),
		bitquery.WithDialer(d.dialer()),
		bitquery.WithRetryPolicy(policy),
	}, extra...)
	c, err := New(bitquery.NewStaticTokenProvider("TEST_TOKEN"), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// serveHandshake waits for connection_init, acks, then waits for the
// subscribe frame and runs the optional onSub callback.
func serveHandshake(conn *fakeConn, onSub func()) {
	conn.waitWrite(1) // connection_init
	conn.push(map[string]any{"type": "connection_ack"})
	conn.waitWrite(2) // subscribe frame
	if onSub != nil {
		onSub()
	}
}

func drainEvents(stream *Stream) []Event {
	var out []Event
	for ev := range stream.Events {
		out = append(out, ev)
	}
	return out
}

func frameType(data []byte) string {
	var m struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &m)
	return m.Type
}

// ---- tests ----------------------------------------------------------

func TestLifecycleTransportWS(t *testing.T) {
	d := &dialRec{}
	var conn *fakeConn
	d.serve = func(c *fakeConn) {
		conn = c
		serveHandshake(c, func() {
			c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"data": map[string]any{"n": 1}}})
			c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"data": map[string]any{"n": 2}}})
			c.push(map[string]any{"type": "complete", "id": "1"})
		})
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(stream)

	var data, complete int
	for _, ev := range events {
		switch ev.Type {
		case EventData:
			data++
		case EventComplete:
			complete++
		}
	}
	if data != 2 || complete != 1 {
		t.Fatalf("events: data=%d complete=%d", data, complete)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	// Handshake frames: init then subscribe.
	conn.waitWrite(2)
	if frameType(d.conns[0].writes[0]) != "connection_init" {
		t.Fatalf("first frame = %s", frameType(d.conns[0].writes[0]))
	}
	if frameType(d.conns[0].writes[1]) != "subscribe" {
		t.Fatalf("second frame = %s", frameType(d.conns[0].writes[1]))
	}
}

func TestLifecycleGraphQLWSProtocol(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		serveHandshake(c, func() {
			c.push(map[string]any{"type": "ka"})
			c.push(map[string]any{"type": "data", "id": "1", "payload": map[string]any{"data": map[string]any{"n": 1}}})
			c.push(map[string]any{"type": "complete", "id": "1"})
		})
	}
	c := newTestClient(t, d, bitquery.WithSubProtocol(bitquery.SubProtocolGraphQLWS))

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(stream)

	var ka, data int
	for _, ev := range events {
		switch ev.Type {
		case EventKeepalive:
			ka++
		case EventData:
			data++
		}
	}
	if ka != 1 || data != 1 {
		t.Fatalf("ka=%d data=%d", ka, data)
	}
	if got := frameType(d.conns[0].writes[1]); got != "start" {
		t.Fatalf("graphql-ws subscribe frame = %q, want start", got)
	}
}

func TestTokenOnlyInURLQuery(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		serveHandshake(c, func() {
			c.push(map[string]any{"type": "complete", "id": "1"})
		})
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(stream)

	if d.count() != 1 {
		t.Fatalf("dials = %d", d.count())
	}
	url := d.urls[0]
	if !strings.Contains(url, "token=TEST_TOKEN") {
		t.Fatalf("token missing from URL: %s", url)
	}
	if !strings.HasPrefix(url, "wss://ws.test/graphql?") {
		t.Fatalf("unexpected ws URL: %s", url)
	}
}

func TestCancelClosesSocketAndStopsWorker(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		// ack + subscribe, then stream forever (no more frames)
		serveHandshake(c, nil)
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	d.conn(t, 0).waitWrite(2)

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	// Close() blocks until the worker exits — socket must be closed.
	if !d.conns[0].isClosed() {
		t.Fatal("socket not closed on Close()")
	}
	// Events channel must be closed now.
	select {
	case _, ok := <-stream.Events:
		if ok {
			t.Fatal("unexpected event after close")
		}
	default:
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("Err() = %v (clean close expected nil)", err)
	}
}

func TestSocketDropReconnects(t *testing.T) {
	d := &dialRec{}
	var first atomic.Bool
	d.serve = func(c *fakeConn) {
		c.waitWrite(1)
		c.push(map[string]any{"type": "connection_ack"})
		c.waitWrite(2)
		if first.CompareAndSwap(false, true) {
			// First connection: deliver one event then drop the socket.
			c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"data": 1}})
			c.pushErr(errors.New("connection reset by peer"))
			return
		}
		// Reconnected socket: deliver and complete.
		c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"data": 2}})
		c.push(map[string]any{"type": "complete", "id": "1"})
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(stream)

	if d.count() != 2 {
		t.Fatalf("dials = %d, want 2 (reconnect)", d.count())
	}
	var data int
	for _, ev := range events {
		if ev.Type == EventData {
			data++
		}
	}
	if data != 2 {
		t.Fatalf("data events = %d, want 2 (one per connection)", data)
	}
}

func TestReconnectBudgetExhausted(t *testing.T) {
	d := &dialRec{err: errors.New("dial refused")}
	c := newTestClient(t, d, bitquery.WithSubscriptionReconnect(2))

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(stream)

	err = stream.Err()
	if err == nil {
		t.Fatal("expected terminal error")
	}
	var be *bitquery.Error
	if !errors.As(err, &be) || be.Kind != bitquery.KindSubscription {
		t.Fatalf("kind = %v", err)
	}
	if !errors.Is(err, bitquery.ErrSubscription) {
		t.Fatal("sentinel mismatch")
	}
	if d.count() != 3 { // initial + 2 retries
		t.Fatalf("dials = %d, want 3", d.count())
	}
}

func TestDialErrorRedactsToken(t *testing.T) {
	d := &dialRec{err: errors.New("refused wss://ws.test/graphql?token=TEST_TOKEN")}
	c := newTestClient(t, d, bitquery.WithSubscriptionReconnect(0))

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(stream)
	if err := stream.Err(); err == nil {
		t.Fatal("expected error")
	} else if strings.Contains(err.Error(), "TEST_TOKEN") {
		t.Fatalf("token leaked in error: %s", err.Error())
	}
}

func TestBoundedQueueDropOldest(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		c.waitWrite(1)
		c.push(map[string]any{"type": "connection_ack"})
		c.waitWrite(2)
		for i := 0; i < 8; i++ {
			c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"i": i}})
		}
		// Let the run loop drain the 8 frames into the bounded queue
		// before completing, so the tail window is deterministic.
		time.Sleep(150 * time.Millisecond)
		c.push(map[string]any{"type": "complete", "id": "1"})
	}
	c := newTestClient(t, d, bitquery.WithSubscriptionQueue(3, bitquery.OverflowDropOldest))

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	// Do NOT consume Events while the queue fills — Wait() blocks until
	// the stream terminates without touching the buffer.
	stream.Wait()
	events := drainEvents(stream)

	if stream.Dropped() == 0 {
		t.Fatal("expected dropped events on a full queue")
	}
	// Queue capacity is 3: only the tail window may be delivered.
	if len(events) > 3 {
		t.Fatalf("delivered events = %d, want <= 3", len(events))
	}
	var lastData *Event
	for i := range events {
		if events[i].Type == EventData {
			lastData = &events[i]
		}
	}
	if lastData == nil {
		t.Fatal("no data events received")
	}
	var payload struct {
		I int `json:"i"`
	}
	_ = json.Unmarshal(lastData.Payload, &payload)
	if payload.I != 7 {
		t.Fatalf("last data event i=%d, want 7 (oldest dropped)", payload.I)
	}
}

func TestOverflowPolicyFail(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		c.waitWrite(1)
		c.push(map[string]any{"type": "connection_ack"})
		c.waitWrite(2)
		for i := 0; i < 10; i++ {
			c.push(map[string]any{"type": "next", "id": "1", "payload": map[string]any{"i": i}})
		}
	}
	c := newTestClient(t, d,
		bitquery.WithSubscriptionQueue(1, bitquery.OverflowFail),
		bitquery.WithSubscriptionReconnect(0),
	)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	// Do NOT consume during the burst — the queue must fill for the
	// overflow policy to fire. Wait() blocks without draining.
	stream.Wait()
	drainEvents(stream)

	err = stream.Err()
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

func TestCompleteDoesNotReconnect(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		serveHandshake(c, func() {
			c.push(map[string]any{"type": "complete", "id": "1"})
		})
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(stream)
	time.Sleep(20 * time.Millisecond) // let any reconnect attempt fire
	if d.count() != 1 {
		t.Fatalf("dials = %d — complete must not reconnect", d.count())
	}
}

func TestConnectionErrorFrame(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		c.waitWrite(1)
		c.push(map[string]any{"type": "connection_error", "payload": map[string]any{"message": "unauthorized"}})
	}
	c := newTestClient(t, d, bitquery.WithSubscriptionReconnect(0))

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(stream)
	err = stream.Err()
	if err == nil || !strings.Contains(err.Error(), "connection_error") {
		t.Fatalf("expected connection_error, got %v", err)
	}
}

func TestSubscribeRejectsEmptyDocumentBeforeDialing(t *testing.T) {
	d := &dialRec{}
	c := newTestClient(t, d)
	_, err := c.Subscribe(context.Background(), bitquery.Operation{})
	if err == nil {
		t.Fatal("expected empty-document error")
	}
	if d.count() != 0 {
		t.Fatal("empty document must not dial")
	}
}

func TestDuplicateConnectionAckIsTerminal(t *testing.T) {
	d := &dialRec{}
	d.serve = func(c *fakeConn) {
		c.waitWrite(1)
		c.push(map[string]any{"type": "connection_ack"})
		c.waitWrite(2)
		c.push(map[string]any{"type": "connection_ack"})
	}
	c := newTestClient(t, d)

	stream, err := c.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Wait()
	if err := stream.Err(); err == nil || !strings.Contains(err.Error(), "duplicate connection_ack") {
		t.Fatalf("duplicate ack must terminate stream, got %v", err)
	}
}
