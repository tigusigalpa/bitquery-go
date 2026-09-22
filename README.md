# Bitquery Golang Client/SDK/Library

![Bitquery Golang SDK Client](https://i.postimg.cc/9QWpFHnn/bitquery-golang-hero-github-sdk.jpg)

[![CI](https://github.com/tigusigalpa/birdeye-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/tigusigalpa/birdeye-go/actions/workflows/ci.yml)
[![Tests](https://github.com/tigusigalpa/birdeye-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/tigusigalpa/birdeye-go/actions/workflows/test.yml)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![CodeQL](https://github.com/tigusigalpa/birdeye-go/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/tigusigalpa/birdeye-go/actions/workflows/codeql.yml)
[![Codecov](https://codecov.io/gh/tigusigalpa/birdeye-go/graph/badge.svg)](https://codecov.io/gh/tigusigalpa/birdeye-go)
[![GitHub Release](https://img.shields.io/github/v/release/tigusigalpa/birdeye-go?style=flat-square)](https://github.com/tigusigalpa/birdeye-go/releases)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue?style=flat-square&logo=go)](https://pkg.go.dev/github.com/tigusigalpa/birdeye-go)

`bitquery-go` is a production-oriented Go SDK for Bitquery GraphQL. It keeps the two Bitquery contracts deliberately separate:

- **V1** is the historical HTTPS GraphQL API.
- **V2** is the streaming GraphQL API: HTTPS queries plus opt-in WebSocket subscriptions.

The library never rewrites a document, changes an endpoint, or falls back from one version to the other. That matters because V1 and V2 have different schemas and coverage.

- Module: `github.com/tigusigalpa/bitquery-go`
- Go: **1.21 or newer** (CI covers Go 1.21–1.26)
- License: MIT — © Igor Sazonov

## Pick the right client first

| If you need… | Use | Important detail |
| --- | --- | --- |
| An existing historical V1 document | `v1.Client` | HTTPS only; no subscription API |
| A new supported EVM or Solana integration | `v2.Client` | Start with the current V2 schema in the Bitquery IDE |
| Live V2 updates | `subscription.Client` | A separate, explicit WebSocket worker |

V1 still has legacy coverage, but Bitquery marks Ethereum, BSC, Matic/Polygon and Tron V1 usage as deprecated. The SDK reports this as a typed notice; it never blocks a deliberate V1 call. V2 is not a drop-in replacement for every V1 dataset. Check the live schema and the [V1/V2 coverage guide](docs/version-coverage.md) before migrating.

## Install

```bash
go get github.com/tigusigalpa/bitquery-go
```

Creating a client makes no network request. Requests happen only when you call `Execute`; a socket is opened only when you call `Subscribe`.

## Authentication: choose one safe source of tokens

Keep credentials in your process environment or your own secret store — never in source code, examples, or logs.

```go
// A pre-minted Bitquery access token.
provider := bitquery.NewStaticTokenProvider(os.Getenv("BITQUERY_TOKEN"))

// Or client credentials. The provider coalesces concurrent refreshes
// and caches the token until shortly before expiry.
provider := bitquery.NewClientCredentialsProvider(
    os.Getenv("BITQUERY_CLIENT_ID"),
    os.Getenv("BITQUERY_CLIENT_SECRET"),
)
```

HTTP requests use `Authorization: Bearer <token>`. WebSocket authentication is different: Bitquery requires the OAuth token in the `?token=` URL query parameter, which this SDK adds internally. Do not put that parameter in a custom endpoint. The SDK redacts it from its errors and logger output.

For a proxy or a test OAuth server, use `bitquery.WithOAuthTokenEndpoint("https://…")` while creating the credentials provider. `bitquery.WithTokenEndpoint` also configures a default `ClientCredentialsProvider` supplied to a client; an explicit OAuth-provider endpoint takes precedence.

## Make an HTTP query

### V2 EVM: generic GraphQL is the main API

Use a raw `Operation` whenever you need a field or cube the helpers do not cover. Values belong in `Variables`, never in string-concatenated GraphQL.

```go
client, err := v2.New(provider, bitquery.WithRegion(bitquery.RegionUS))
if err != nil { return err }

resp, err := client.Execute(ctx, bitquery.Operation{
    OperationName: "LatestBlocks",
    Query: `query LatestBlocks($network: evm_network!) {
        EVM(network: $network) {
            Blocks(limit: {count: 3}) { Block { Number Time } }
        }
    }`,
    Variables: map[string]any{"network": "eth"},
})
if err != nil { return err }
```

There are deliberately thin helpers for a few documented common cases; they still produce an ordinary `Operation` that you can inspect or modify:

```go
resp, err := client.Execute(ctx, v2.EVMDexTrades(bitquery.NetworkBSC, 20, false))
```

### V2 Solana: use the Solana cube

```go
client, err := v2.New(provider)
if err != nil { return err }

op := v2.SolanaTransfers(25, os.Getenv("SOLANA_MINT")) // a non-empty mint address
resp, err := client.Execute(ctx, op)
if err != nil { return err }
```

The helper set includes EVM DEX trades, transfers and transactions, and Solana DEX trades, transfers, balance updates, instructions and transactions. It intentionally does **not** generate a brittle model of every evolving Bitquery cube.

### V1 historical query: explicit and inspectable

```go
client, err := v1.New(provider)
if err != nil { return err }

op := v1.Blocks(bitquery.NetworkEthereum, 10, "", "")
for _, notice := range client.DeprecationNotices(op) {
    log.Printf("%s: %s", notice.Network, notice.Message)
}

resp, err := client.Execute(ctx, op)
if err != nil { return err }
```

V1 helpers cover blocks, transactions, transfers, DEX trades and smart contract calls. They are conveniences, not a replacement for `Execute`.

## Read responses without losing numeric precision

Bitquery amounts, decimals, block heights and IDs can exceed the safe range of `float64`. The SDK leaves `Data` as `json.RawMessage` and uses `json.Number` when decoding into `interface{}` values.

```go
if resp.HasErrors() {
    // HTTP 200 can still contain GraphQL errors.
    for _, graphQLError := range resp.Errors {
        log.Printf("GraphQL: %s", graphQLError.Message)
    }
}
if resp.HasPartialData() {
    // Some data is still usable; decide case by case.
}

var data map[string]any
if err := resp.DecodeData(&data); err != nil { return err }
// Convert an amount explicitly with math/big or a decimal package.
```

The default is tolerant: GraphQL `errors[]` live on `Response`. Use `ExecuteStrict` (or `bitquery.WithStrict()`) if your application wants a `KindGraphQL` error instead. Even then, the error retains the complete response so partial data is not discarded.

## Regions and endpoint overrides

The default is Europe. Choose the closest region for your deployment:

```go
client, err := v2.New(provider,
    bitquery.WithRegion(bitquery.RegionAsia),
    bitquery.WithTimeout(20*time.Second),
    bitquery.WithUserAgent("my-indexer/1.0"),
)
```

| Region | V1 HTTPS | V2 HTTPS | V2 WebSocket |
| --- | --- | --- | --- |
| Europe (default) | `https://graphql.bitquery.io` | `https://streaming.bitquery.io/graphql` | `wss://streaming.bitquery.io/graphql` |
| Asia | `https://asia.graphql.bitquery.io` | `https://asia.streaming.bitquery.io/graphql` | `wss://asia.streaming.bitquery.io/graphql` |
| US | `https://us.graphql.bitquery.io` | `https://us.streaming.bitquery.io/graphql` | `wss://us.streaming.bitquery.io/graphql` |

`WithBaseURL` overrides the selected HTTP endpoint. For a private proxy or a local test socket, use `WithWebSocketURL`; it overrides the derived WSS endpoint. Only absolute HTTP(S) and WS(S) URLs are accepted, and credential-bearing URL parameters are rejected.

`Network` is an open string type, not a closed list. Bitquery coverage varies by region, plan, dataset and cube, so verify unfamiliar networks in the Bitquery schema instead of waiting for an SDK release.

## Retries, limits, and cancellation

The default retry policy is intentionally conservative: up to four attempts with an approximately 5-second exponential backoff, capped at 60 seconds and jittered. `Retry-After` wins when Bitquery supplies it. It handles transient network errors, 429, temporary 5xx responses and documented shared-compute blocks.

Only read operations are retried. Mutations and HTTP subscriptions are never replayed automatically — whether they are safe to repeat is your decision. Pass `bitquery.WithRetryPolicy(bitquery.NoRetry())` to turn off SDK retries.

```go
client, err := v2.New(provider,
    bitquery.WithRateLimiter(bitquery.NewRateLimiter(30)), // 30 req/min burst + pace
    bitquery.WithTimeout(20*time.Second),
)
```

`NewRateLimiter(0)` is a convenient no-op. The SDK does not fan out or parallelize heavy queries; use your own worker limits and respect your Bitquery plan's concurrency allowance. Every request and reconnect obeys the supplied `context.Context`.

## Subscribe to V2 updates

Run subscriptions in a long-lived worker, not a short HTTP handler. Cancellation closes the WebSocket, which is the way Bitquery ends a stream. Delivery is at-least-once and can be unordered across block portions, so persist a stable event key and deduplicate downstream.

```go
workerCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

client, err := subscription.New(provider,
    bitquery.WithSubProtocol(bitquery.SubProtocolGraphQLTransportWS),
    bitquery.WithSubscriptionQueue(500, bitquery.OverflowDropOldest),
)
if err != nil { return err }

stream, err := client.Subscribe(workerCtx, bitquery.Operation{
    Query: `subscription {
        EVM(network: eth) { Blocks { Block { Number Time } } }
    }`,
})
if err != nil { return err }
defer stream.Close()

for event := range stream.Events {
    switch event.Type {
    case subscription.EventData:
        // Deduplicate event.Payload before writing it anywhere.
    case subscription.EventKeepalive:
        // The connection is healthy.
    case subscription.EventComplete:
        // The server completed this operation.
    }
}
if err := stream.Err(); err != nil { return err }
```

Both `graphql-transport-ws` and `graphql-ws` are supported. The stream handles `connection_init`/acknowledgement, `next`/`data`, `ping`/`pong` and `ka`, bounded reconnects, and clean shutdown. Its bounded event queue protects memory: `drop_oldest` keeps the newest events and reports the count via `Dropped()`; `fail` stops the stream rather than losing an event silently. See [the lifecycle guide](docs/websocket-lifecycle.md) for the state machine and recovery checklist.

## Errors you can act on

All SDK failures use `*bitquery.Error`; `errors.Is` and `errors.As` work as expected.

```go
var apiErr *bitquery.Error
if errors.As(err, &apiErr) {
    switch apiErr.Kind {
    case bitquery.KindAuthentication:  // 401 or OAuth rejection
    case bitquery.KindAuthorization:   // 403
    case bitquery.KindPlanEntitlement: // 402; do not retry
    case bitquery.KindRateLimited:     // 429; inspect RetryAfter
    case bitquery.KindServer:          // temporary for 500/502/503/504
    case bitquery.KindGraphQL:         // strict mode; Response is retained
    case bitquery.KindSubscription:
    }
}
```

Diagnostic messages and the built-in structured logger redact Bearer tokens, OAuth secrets and URL `token` parameters. The logger is a no-op unless you supply one with `WithLogger`.

## Runnable examples

Every example compiles without a credential and only contacts Bitquery when you run it with the required environment variables.

| Example | Run | Shows |
| --- | --- | --- |
| [`examples/http`](examples/http) | `BITQUERY_TOKEN=… go run ./examples/http` | V2 EVM HTTP query and partial GraphQL errors |
| [`examples/oauth`](examples/oauth) | `BITQUERY_CLIENT_ID=… BITQUERY_CLIENT_SECRET=… go run ./examples/oauth` | client-credentials token provider |
| [`examples/subscription`](examples/subscription) | `BITQUERY_TOKEN=… go run ./examples/subscription` | graceful V2 subscription worker |
| [`examples/v1-historical`](examples/v1-historical) | `BITQUERY_TOKEN=… go run ./examples/v1-historical` | V1 helper and deprecation signal |
| [`examples/v2-solana`](examples/v2-solana) | `BITQUERY_TOKEN=… go run ./examples/v2-solana` | V2 Solana transfers helper |
| [`examples/custom-endpoint`](examples/custom-endpoint) | `BITQUERY_TOKEN=… go run ./examples/custom-endpoint` | region, endpoint, timeout and user-agent options |

## Reference and project docs

- [V1/V2 coverage and official Bitquery URLs](docs/version-coverage.md)
- [Architecture and public boundaries](docs/architecture.md)
- [HTTP errors, partial responses and retries](docs/error-handling.md)
- [WebSocket lifecycle and backpressure](docs/websocket-lifecycle.md)
- [Why `coder/websocket`](docs/adr/0001-websocket-library.md)
- [Contributing](CONTRIBUTING.md) and [security reporting](SECURITY.md)

## Verify a checkout

```bash
go build ./...
gofmt -l .            # no output means formatted
go vet ./...
go test ./...         # no network
go test -race ./...   # concurrency suite
go mod verify
```

Live smoke tests are opt-in only: set both `BITQUERY_LIVE=1` and `BITQUERY_TOKEN`. They never run in public CI.
