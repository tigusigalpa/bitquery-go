# Changelog

All notable changes to `bitquery-go` are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] — Unreleased

### Added
- Explicitly separated **V1** (`v1` package) and **V2** (`v2` package)
  GraphQL clients — no document rewriting or endpoint substitution.
- Region-aware endpoint resolution (europe/asia/us) with explicit
  `WithBaseURL` / `WithWebSocketURL` overrides.
- `TokenProvider` contract: `StaticTokenProvider` and OAuth2
  `ClientCredentialsProvider` (cached to expiry, coalesced refreshes,
  401 auto-refresh on first failure).
- HTTP GraphQL executor: tolerant/strict modes, partial-data handling,
  precision-safe decoding (`json.RawMessage` + `json.Number`).
- Typed error taxonomy (`*Error` + `Kind` + `errors.Is` sentinels) with
  sanitized messages/context — secrets never leak.
- Retry policy (5s→60s exponential + jitter, `Retry-After` honoured,
  429/5xx/network/shared-compute transient) and client-side token-bucket
  rate limiter.
- Opt-in V2 WebSocket `subscription.Client`: `graphql-ws` and
  `graphql-transport-ws`, reconnect with bounded backoff, bounded event
  queue with `drop_oldest`/`fail` policies, `Dropped()` observability,
  `Close()`/`Wait()`/`Err()` lifecycle.
- V1 deprecation notices as typed signals for ethereum/bsc/matic/tron.
- `WithDialer`/`WSConn` test seams; race-tested suite (`go test -race`).

### Changed
- Retries now replay only conservative read operations; mutations and
  HTTP subscriptions are never retried by the SDK.
- Default HTTP transports are owned once per executor rather than
  created for every request.
- Endpoint, retry, token, subscription and queue configuration now fail
  early with typed configuration errors.
- Short-lived OAuth tokens retain a proportional safety margin instead
  of being immediately considered expired.
- README and runnable examples now cover V1, V2 EVM, V2 Solana, OAuth,
  endpoint configuration and the subscription worker lifecycle.

### Fixed
- A zero or negative client-side request limit is now a documented no-op
  rather than a possible wait loop.
- `Retry-After` HTTP dates now use the injectable clock used by tests.
- OAuth responses with a non-2xx status are rejected even if they carry
  a token-shaped JSON body.
