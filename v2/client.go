// Package v2 provides the Bitquery V2 streaming GraphQL client (HTTPS).
//
// For realtime subscriptions see package subscription — an explicit,
// opt-in WebSocket client, never an automatic feature of this client.
//
// https://docs.bitquery.io/docs/graphql/query/
package v2

import (
	"context"

	"github.com/tigusigalpa/bitquery-go"
)

// Client is the Bitquery V2 streaming GraphQL client (HTTPS queries).
type Client struct {
	exec *bitquery.Executor
}

// New builds a V2 client. The API version is fixed — never ambiguous.
func New(provider bitquery.TokenProvider, opts ...bitquery.Option) (*Client, error) {
	opts = append([]bitquery.Option{bitquery.WithTokenProvider(provider)}, opts...)
	exec, err := bitquery.NewExecutor(bitquery.V2, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{exec: exec}, nil
}

// Execute runs any valid V2 GraphQL document.
func (c *Client) Execute(ctx context.Context, op bitquery.Operation) (*bitquery.Response, error) {
	return c.exec.Execute(ctx, op)
}

// ExecuteStrict fails on any errors[] in the response.
func (c *Client) ExecuteStrict(ctx context.Context, op bitquery.Operation) (*bitquery.Response, error) {
	return c.exec.ExecuteStrict(ctx, op)
}

// Endpoint returns the resolved V2 endpoint.
func (c *Client) Endpoint() (string, error) { return c.exec.Endpoint() }
