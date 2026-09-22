# ADR 0001 — WebSocket client library: `github.com/coder/websocket`

- Status: accepted
- Date: 2025
- Context: `subscription` package needs a Go WebSocket client for the
  Bitquery V2 streaming endpoint.

## Decision

Use **`github.com/coder/websocket v1.8.12`** (the community-maintained
successor of `nhooyr.io/websocket`).

## Candidates considered

| Library | Verdict |
| --- | --- |
| `github.com/coder/websocket` | **chosen** — context-first API, RFC 6455 strict, actively maintained, minimal deps |
| `github.com/gorilla/websocket` | rejected — `ReadMessage`/`WriteMessage` API predates contexts; cancellation needs manual `SetReadDeadline` juggling; project is archived/frozen |
| `github.com/coder/websocket` fork risk | mitigated — the dependency is isolated behind the `bitquery.WSConn`/`bitquery.Dialer` contracts |

## Rationale

- **Context-native `Read`/`Write`** — `Stream.Close()` cancels the
  context and a blocked `Read` returns immediately; no deadline hacks
  needed for clean worker shutdown.
- **Subprotocol negotiation** is first-class (`DialOptions.Subprotocols`)
  — required for `graphql-ws` / `graphql-transport-ws`.
- **Small surface**: one import in `subscription/conn.go`; everything
  else programs against `bitquery.WSConn`, so tests use fakes and a
  future swap touches one file.
- Version **pinned** in `go.mod` (`v1.8.12`, published >7 days before
  adoption); `go mod verify` runs in CI.

## Consequences

- Consumers never interact with `coder/websocket` directly — only via
  `subscription.Client` / `bitquery.WSConn`.
- A different dialer (proxy-aware transports, exotic TLS) can be
  injected via `bitquery.WithDialer` without touching this package.
