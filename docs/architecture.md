# Architecture

```
bitquery-go
├── (root)            shared contract — no version leakage
│   ├── types.go      ApiVersion, Region, Network, Operation, Response, GraphQLError
│   ├── endpoints.go  region/version → HTTPS/WSS resolution (override wins)
│   ├── config.go     Config + functional options (region, endpoints, HTTP client,
│   │                 retry, rate limiter, logger, strict, subscription tuning)
│   ├── token.go      TokenProvider iface · StaticTokenProvider ·
│   │                 ClientCredentialsProvider (OAuth2, cached, coalesced)
│   ├── executor.go   HTTP GraphQL execution: auth, read-only retry loop, response parsing
│   ├── retry.go      RetryPolicy — 5s→60s exp. backoff + jitter + Retry-After
│   ├── ratelimit.go  token-bucket RateLimiter (client-side pacing)
│   ├── errors.go     *Error + Kind taxonomy + sentinels (errors.Is/As)
│   ├── logger.go     Logger iface (default no-op)
│   ├── wsconn.go     WSConn/Dialer contracts for the subscription transport
│   └── internal/redact/  secret scrubbing for every observability path
├── v1/               V1 client — HTTPS only, explicit version
│   ├── client.go     v1.Client + DeprecationNotices (typed signal)
│   └── queries.go    typed V1 helper builders
├── v2/               V2 client — HTTPS only, explicit version
│   ├── client.go     v2.Client
│   └── queries.go    typed V2 helper builders (EVM, Solana, …)
└── subscription/     V2 WebSocket client — OPT-IN, separate type
    ├── conn.go       coder/websocket adapter (production dialer)
    └── client.go     handshake → frames → bounded queue → Events chan
```

## Invariants

1. **V1 ≠ V2.** The version is fixed at client construction
   (`bitquery.NewExecutor(version, …)`); nothing rewrites documents or
   swaps endpoints. `v1` and `v2` are separate packages with separate
   client types.
2. **No network at init.** `New`/`NewConfig` build configuration only.
   Sockets and HTTP calls happen in `Execute`/`Subscribe` only.
3. **Subscriptions are opt-in.** `subscription.Client` is a separate
   constructor — importing `v2` never opens a socket.
4. **Secrets never leak.** Tokens travel in `Authorization: Bearer`
   (HTTP) or `?token=` (WSS, the only Bitquery-supported method). All
   error messages and log paths pass through `internal/redact`.
5. **Precision is preserved.** `Response.Data` stays `json.RawMessage`;
   `DecodeData` uses `json.Number`. No float64 coercion anywhere.
6. **Race-safe.** `Stream` state is mutex/atomic-guarded; retry jitter
   is synchronized; `go test -race` is part of the required CI suite.

## Subscription lifecycle

```
dial wss://…/graphql?token=…   (token in URL only — per Bitquery docs)
  → write connection_init
  → read connection_ack        → write subscribe (or "start" for graphql-ws)
  → read next/data frames      → enqueue Event{EventData}
  → read ka / ping             → keepalive event / write pong
  → read complete              → Event{EventComplete}, stream ends
  → socket drop/error          → reconnect with bounded backoff
  → Close() / ctx cancel       → close socket (only way to end a stream)
```

- Both subprotocols: `graphql-transport-ws` (`subscribe`/`next`,
  `ping`/`pong`) and `graphql-ws` (`start`/`data`, `ka`).
- Reconnect budget: `SubscriptionMaxReconnects` consecutive failures;
  counter resets after a healthy (acknowledged) connection.
- Bounded queue: `SubscriptionQueueCapacity`; overflow policy
  `drop_oldest` (default, counted via `Dropped()`) or `fail` (terminates
  the stream — never reconnects).
- `connection_error` frames are treated as terminal — retrying a
  rejected handshake is pointless.

## Testability seams

- `WithHTTPClient` — inject any `*http.Client`.
- `WithDialer` + `WSConn` — fake sockets without a network (see
  `subscription/client_test.go`).
- `RetryPolicy.Sleep` / `RetryPolicy.Rand` — deterministic backoff.
- `TokenBucketRateLimiter` — injectable clock/sleep for tests.
