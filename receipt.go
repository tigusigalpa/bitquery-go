package bitquery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/tigusigalpa/bitquery-go/internal/redact"
)

// ReceiptSource identifies the transport that produced a raw receipt.
type ReceiptSource string

const (
	// ReceiptSourceHTTP identifies an HTTP GraphQL response.
	ReceiptSourceHTTP ReceiptSource = "http"
	// ReceiptSourceWebSocket identifies a GraphQL-over-WebSocket frame.
	ReceiptSourceWebSocket ReceiptSource = "websocket"
)

// ReceiptDirection identifies whether an observed WebSocket frame was
// received from or sent to Bitquery. HTTP receipts are always received.
type ReceiptDirection string

const (
	// ReceiptReceived identifies a response or inbound WebSocket frame.
	ReceiptReceived ReceiptDirection = "received"
	// ReceiptSent identifies an outbound WebSocket protocol frame.
	ReceiptSent ReceiptDirection = "sent"
)

// Receipt is an immutable snapshot that correlates a raw HTTP response or
// WebSocket frame with the exact serialized GraphQL operation that produced
// it. Target is redacted before storage; Operation and Raw return defensive
// copies so callers cannot alter the captured evidence.
//
// The SDK does not persist receipts or infer delivery completeness. Applications
// that need durable lineage must store the returned snapshots themselves.
type Receipt struct {
	Source          ReceiptSource
	Direction       ReceiptDirection
	Target          string
	CapturedAt      time.Time
	StatusCode      int
	OperationSHA256 string

	operation json.RawMessage
	raw       json.RawMessage
}

// ReceiptObserver synchronously receives a captured subscription receipt. It
// runs on the subscription worker without internal Stream locks, so it may
// apply backpressure deliberately. Implementations must honour ctx and return
// promptly; they must not call Stream.Close or Stream.Wait synchronously from
// the callback, because both wait for that worker to exit.
//
// Returning an error terminates the stream without reconnecting and records an
// observable gap. The terminal typed error retains the triggering Receipt. The
// callback receives an immutable Receipt and may retain it.
type ReceiptObserver func(ctx context.Context, receipt Receipt) error

// NewReceipt captures a raw transport observation. operation must be the JSON
// representation of a GraphQL Operation; raw is the exact HTTP body or
// WebSocket frame. The constructor copies both values and redacts credential
// query parameters from target.
func NewReceipt(source ReceiptSource, direction ReceiptDirection, target string, operation, raw []byte, statusCode int, capturedAt time.Time) Receipt {
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	} else {
		capturedAt = capturedAt.UTC()
	}
	hash := sha256.Sum256(operation)
	return Receipt{
		Source:          source,
		Direction:       direction,
		Target:          redact.URLQuery(target),
		CapturedAt:      capturedAt,
		StatusCode:      statusCode,
		OperationSHA256: hex.EncodeToString(hash[:]),
		operation:       append(json.RawMessage(nil), operation...),
		raw:             append(json.RawMessage(nil), raw...),
	}
}

// Operation returns a copy of the exact serialized GraphQL operation.
func (r Receipt) Operation() json.RawMessage {
	return append(json.RawMessage(nil), r.operation...)
}

// Raw returns a copy of the exact HTTP response body or WebSocket frame.
func (r Receipt) Raw() json.RawMessage {
	return append(json.RawMessage(nil), r.raw...)
}

// Optional represents a GraphQL variable with three distinct states:
// absent (the zero value), explicitly null, or a concrete value. It is used
// by pinned helpers where server defaults and GraphQL null have different
// meanings.
type Optional[T any] struct {
	// Set distinguishes an absent variable from an explicit GraphQL null.
	Set bool
	// Value is nil when Set represents GraphQL null.
	Value *T
}

// Present returns an Optional containing value.
func Present[T any](value T) Optional[T] {
	return Optional[T]{Set: true, Value: &value}
}

// Null returns an Optional that serializes as an explicit GraphQL null.
func Null[T any]() Optional[T] { return Optional[T]{Set: true} }

// SetVariable adds the optional value to variables only when it is present.
// It deliberately does not use omitempty: an absent variable and a null
// variable have different GraphQL semantics.
func (o Optional[T]) SetVariable(variables map[string]any, name string) {
	if !o.Set {
		return
	}
	if o.Value == nil {
		variables[name] = nil
		return
	}
	variables[name] = *o.Value
}
