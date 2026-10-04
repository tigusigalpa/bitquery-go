# WebSocket subscription lifecycle

`subscription.Client` is intentionally separate from `v2.Client`. It is for a long-lived worker that consumes V2 GraphQL subscriptions, not for a short synchronous HTTP request.

## Lifecycle

```text
resolve V2 WSS endpoint
  → obtain OAuth token
  → dial wss://…/graphql?token=… with selected GraphQL subprotocol
  → connection_init
  → connection_ack
  → subscribe (or start for graphql-ws)
  → next/data events, ka/pong keepalives, and ping→pong replies
  → complete OR socket error OR caller cancellation
  → close socket
```

The token is placed only in the URL query string as required by Bitquery. It is never copied to an `Authorization` WebSocket header, error message, or library logger.

## Choosing a protocol

`graphql-transport-ws` is the default. Set `WithSubProtocol(SubProtocolGraphQLWS)` for the legacy `graphql-ws` protocol. The client negotiates exactly the selected protocol and sends the compatible subscribe frame:

| Protocol | Start frame | Result frame | Keepalive |
| --- | --- | --- | --- |
| `graphql-transport-ws` | `subscribe` | `next` | `pong`, plus `ping` → `pong` |
| `graphql-ws` | `start` | `data` | `ka` |

## Backpressure and shutdown

The event channel is bounded. Configure it with:

```go
bitquery.WithSubscriptionQueue(500, bitquery.OverflowDropOldest)
```

- `drop_oldest` evicts the oldest buffered event and increments `Stream.Dropped()`.
- `fail` stops the stream with a typed `KindSubscription` error instead of losing an event. It also records a `GapOverflowFail` entry in `Stream.Gaps()`; persist that evidence in the caller's ingestion layer if completeness matters.

If a `complete` frame cannot enter a `fail` queue, it is also terminal rather
than a clean completion. The terminal error retains that frame's raw receipt;
it does not reconnect or claim that the subscription completed successfully.

`drop_oldest` is appropriate only when loss is acceptable. For
completeness-sensitive ingestion, choose `OverflowFail`; a dropped-event
counter cannot establish coverage or reconstruct a missing delivery.

Always stop the worker with a cancellable context and call `Stream.Close()` on early return. Bitquery does not end a stream through a GraphQL close message; the socket must be closed.

## Receipts and delivery evidence

`Stream.Receipts()` returns a bounded in-memory buffer of immutable inbound
and outbound WebSocket frame snapshots. The default holds 1,024 entries and
uses `OverflowFail`: a full buffer terminates the stream and records
`GapReceiptOverflow` rather than silently losing evidence. Configure the buffer
with `WithSubscriptionReceiptBuffer`, or call `DrainReceipts()` regularly to
transfer ownership and release retained memory. The typed terminal error keeps
the raw receipt that triggered fail-closed admission, even though it could not
enter the full retained buffer.

For continuous ingestion, `WithSubscriptionReceiptObserver` invokes a callback
synchronously on the connection worker before it retains a frame. The callback
is the explicit backpressure boundary: it owns the immutable `Receipt`, must
honour the provided context, return promptly, and must not call `Close` or
`Wait` synchronously. Its error is terminal, records `GapReceiptObserver`, and
retains the triggering receipt on the typed error.
Set receipt-buffer capacity to zero when the observer is the only retention
path. `Event.Receipt` is the matching inbound-frame receipt for delivered
events.

`Event.Delivery` has a stream-wide receive sequence and connection epoch. The
first data event after a reconnect is marked `ReconnectGap: true`, and
`Stream.Gaps()` records a `GapReconnect` condition. These are observable signs
that delivery may be discontinuous. They are not proof of loss, do not replay
anything, and do not provide a completeness guarantee or durable persistence.
`DroppedReceipts()` and `DroppedGaps()` report whether explicitly configured
`drop_oldest` retention has evicted receipts or older gap records.

## Recovery checklist

1. Treat each delivery as at-least-once and portions as unordered. Store a deduplication key before handling a payload.
2. On a temporary drop, the client reconnects with bounded exponential backoff. A successfully acknowledged connection resets the consecutive-failure budget.
3. After a longer outage, backfill the missed interval with a V2 HTTP query before trusting the live stream again.
4. If a connected socket stays silent, inspect the plan's concurrent-subscription cap and test the exact document in the Bitquery IDE.
5. `connection_error`, a duplicate acknowledgement, malformed lifecycle order, or queue overflow under `fail` are terminal errors and are not retried blindly.

Relevant Bitquery references: [WebSocket subscriptions](https://docs.bitquery.io/docs/subscriptions/websockets/), [WebSocket authentication](https://docs.bitquery.io/docs/authorization/websocket/), and [subscription examples](https://docs.bitquery.io/docs/subscriptions/examples/).
