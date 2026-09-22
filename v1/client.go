// Package v1 provides the Bitquery V1 historical GraphQL client.
//
// V1 is a separate legacy contract from V2 — different schema and
// endpoint, no subscription support. The V1 docs carry a deprecation
// notice for ethereum, bsc, matic and tron; the SDK surfaces this as a
// typed, inspectable signal without blocking calls.
//
// https://docs.bitquery.io/v1/docs/building-queries/basic-structure-of-a-query
// https://docs.bitquery.io/v1/docs/graphql-ide/v1-and-v2
package v1

import (
	"context"
	"strings"

	"github.com/tigusigalpa/bitquery-go"
)

// Client is the Bitquery V1 historical GraphQL client (HTTPS only).
type Client struct {
	exec *bitquery.Executor
}

// New builds a V1 client. The API version is fixed — never ambiguous.
func New(provider bitquery.TokenProvider, opts ...bitquery.Option) (*Client, error) {
	opts = append([]bitquery.Option{bitquery.WithTokenProvider(provider)}, opts...)
	exec, err := bitquery.NewExecutor(bitquery.V1, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{exec: exec}, nil
}

// Execute runs any valid V1 GraphQL document.
func (c *Client) Execute(ctx context.Context, op bitquery.Operation) (*bitquery.Response, error) {
	return c.exec.Execute(ctx, op)
}

// ExecuteStrict fails on any errors[] in the response.
func (c *Client) ExecuteStrict(ctx context.Context, op bitquery.Operation) (*bitquery.Response, error) {
	return c.exec.ExecuteStrict(ctx, op)
}

// Endpoint returns the resolved V1 endpoint.
func (c *Client) Endpoint() (string, error) { return c.exec.Endpoint() }

// DeprecationNotices returns typed deprecation signals for every
// deprecated V1 network referenced by the operation's variables.
func (c *Client) DeprecationNotices(op bitquery.Operation) []DeprecationNotice {
	return noticesForVariables(op.Variables)
}

// DeprecationNotice is an inspectable signal: V1 marks ethereum, bsc,
// matic and tron as deprecated "as of August 10" (no year in the notice).
type DeprecationNotice struct {
	Network string
	Message string
	DocsURL string
}

const deprecationDocsURL = "https://docs.bitquery.io/v1/docs/graphql-ide/v1-and-v2"

var deprecatedNetworks = map[string]bool{
	"ethereum": true, "eth": true, "bsc": true, "matic": true, "tron": true,
}

// NoticesForNetwork returns the deprecation notice for a network, if any.
func NoticesForNetwork(network string) []DeprecationNotice {
	network = strings.ToLower(strings.TrimSpace(network))
	if !deprecatedNetworks[network] {
		return nil
	}
	return []DeprecationNotice{{
		Network: network,
		Message: "Bitquery V1 marks the \"" + network + "\" network as deprecated. " +
			"Calls still work, but consider the V2 streaming API for new integrations. " +
			"V1 and V2 are separate contracts — verify schema coverage before migrating.",
		DocsURL: deprecationDocsURL,
	}}
}

// noticesForVariables scans variables recursively for "network" keys.
func noticesForVariables(vars map[string]any) []DeprecationNotice {
	var out []DeprecationNotice
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if k == "network" {
					if s, ok := val.(string); ok && !seen[s] {
						for _, n := range NoticesForNetwork(s) {
							seen[s] = true
							out = append(out, n)
						}
					}
				}
				walk(val)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(vars)
	return out
}
