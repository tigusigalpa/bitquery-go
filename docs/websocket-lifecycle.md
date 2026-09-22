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
- `fail` stops the stream with a typed `KindSubscription` error instead of losing an event.

Always stop the worker with a cancellable context and call `Stream.Close()` on early return. Bitquery does not end a stream through a GraphQL close message; the socket must be closed.

## Recovery checklist

1. Treat each delivery as at-least-once and portions as unordered. Store a deduplication key before handling a payload.
2. On a temporary drop, the client reconnects with bounded exponential backoff. A successfully acknowledged connection resets the consecutive-failure budget.
3. After a longer outage, backfill the missed interval with a V2 HTTP query before trusting the live stream again.
4. If a connected socket stays silent, inspect the plan's concurrent-subscription cap and test the exact document in the Bitquery IDE.
5. `connection_error`, a duplicate acknowledgement, malformed lifecycle order, or queue overflow under `fail` are terminal errors and are not retried blindly.

Relevant Bitquery references: [WebSocket subscriptions](https://docs.bitquery.io/docs/subscriptions/websockets/), [WebSocket authentication](https://docs.bitquery.io/docs/authorization/websocket/), and [subscription examples](https://docs.bitquery.io/docs/subscriptions/examples/).
