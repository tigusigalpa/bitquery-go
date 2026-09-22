package subscription

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/tigusigalpa/bitquery-go"
)

// coderConn adapts github.com/coder/websocket to the WSConn contract.
type coderConn struct{ c *websocket.Conn }

func (c *coderConn) Read(ctx context.Context) (bitquery.WSMessageType, []byte, error) {
	mt, data, err := c.c.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	if mt == websocket.MessageBinary {
		return bitquery.WSMessageBinary, data, nil
	}
	return bitquery.WSMessageText, data, nil
}

func (c *coderConn) Write(ctx context.Context, mt bitquery.WSMessageType, data []byte) error {
	wt := websocket.MessageText
	if mt == bitquery.WSMessageBinary {
		wt = websocket.MessageBinary
	}
	return c.c.Write(ctx, wt, data)
}

func (c *coderConn) Close(code uint32, reason string) error {
	return c.c.Close(websocket.StatusCode(code), reason)
}

// CoderDialer dials with github.com/coder/websocket — the production
// dialer. See docs/adr/0001-websocket-library.md.
func CoderDialer(ctx context.Context, url string, subprotocols []string) (bitquery.WSConn, error) {
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: subprotocols,
		HTTPHeader: http.Header{
			"Content-Type": []string{"application/json"},
		},
	})
	if err != nil {
		return nil, err
	}
	return &coderConn{c: conn}, nil
}
