package bitquery

import "context"

// WSMessageType is the WebSocket frame type.
type WSMessageType int

const (
	WSMessageText   WSMessageType = 1
	WSMessageBinary WSMessageType = 2
)

// WSConn abstracts a WebSocket connection so the subscription client can
// be tested without a network. Implementations must honour ctx on Read.
type WSConn interface {
	Read(ctx context.Context) (WSMessageType, []byte, error)
	Write(ctx context.Context, mt WSMessageType, data []byte) error
	// Close terminates the socket — the only way to end a Bitquery stream.
	Close(code uint32, reason string) error
}

// Dialer establishes a WebSocket connection to url requesting the given
// Sec-WebSocket-Protocol values. The url may carry a `token` query
// parameter — never log it raw.
type Dialer func(ctx context.Context, url string, subprotocols []string) (WSConn, error)
