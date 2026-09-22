# Version coverage — V1 versus V2

Bitquery exposes two separate contracts. This SDK preserves that boundary with different client types and endpoints; it does not translate documents or silently fall back between API versions.

## Decision table

| Capability | V1 | V2 |
| --- | --- | --- |
| Purpose | Historical GraphQL and legacy coverage | Streaming GraphQL; historical/realtime availability varies by chain |
| HTTP client | `v1.Client` | `v2.Client` |
| WebSocket client | Not available | `subscription.Client`, explicit opt-in |
| Endpoint family | `graphql.bitquery.io` | `streaming.bitquery.io/graphql` |
| Schema surface | V1 domains such as `ethereum` | V2 cubes such as `EVM` and `Solana` |
| Migration | Manual query-by-query validation | Never assumed to be complete |

The V1 docs display a deprecation notice for Ethereum, BSC, Matic/Polygon and Tron. `v1.Client.DeprecationNotices` and `v1.NoticesForNetwork` expose it as a typed signal without blocking a call. V1 remains a valid explicit choice for legacy contracts.

## Endpoint defaults

| Region | V1 HTTPS | V2 HTTPS | V2 WSS |
| --- | --- | --- | --- |
| Europe (default) | `https://graphql.bitquery.io` | `https://streaming.bitquery.io/graphql` | `wss://streaming.bitquery.io/graphql` |
| Asia | `https://asia.graphql.bitquery.io` | `https://asia.streaming.bitquery.io/graphql` | `wss://asia.streaming.bitquery.io/graphql` |
| US | `https://us.graphql.bitquery.io` | `https://us.streaming.bitquery.io/graphql` | `wss://us.streaming.bitquery.io/graphql` |

An explicit `WithBaseURL` or `WithWebSocketURL` wins over region selection. WSS is normally derived from the V2 HTTPS endpoint by replacing `https` with `wss`. The OAuth client-credentials endpoint is `https://oauth2.bitquery.io/oauth2/token` for all regions.

## What the SDK promises — and what it does not

- `Network` is an open string type. There is no static assertion that every network is supported, because availability depends on version, region, plan, dataset, cube and field.
- A field that works over V2 HTTP is not automatically subscription-compatible. Validate the exact subscription in the Bitquery IDE.
- Realtime messages are at-least-once and unordered across block portions. Consumers must deduplicate with a domain-specific event key.
- V1 helper factories are limited to blocks, transactions, transfers, DEX trades and contract calls. V2 helpers cover a small EVM/Solana subset. Raw `Execute` remains the compatibility escape hatch.

## Official documentation used by this SDK

Core API and migration:

1. [Endpoints and regions](https://docs.bitquery.io/docs/start/endpoints/)
2. [Generate an API token](https://docs.bitquery.io/docs/authorization/how-to-generate/)
3. [Use an API token over HTTP](https://docs.bitquery.io/docs/authorization/how-to-use/)
4. [WebSocket token authentication](https://docs.bitquery.io/docs/authorization/websocket/)
5. [WebSocket subscriptions and supported standards](https://docs.bitquery.io/docs/subscriptions/websockets/)
6. [WebSocket lifecycle examples](https://docs.bitquery.io/docs/subscriptions/examples/)
7. [Rate limits, concurrency and backoff](https://docs.bitquery.io/docs/plans/rate-limits/)
8. [Errors and diagnostics](https://docs.bitquery.io/docs/start/errors/)
9. [V1 and V2 comparison](https://docs.bitquery.io/v1/docs/graphql-ide/v1-and-v2)
10. [V1 to V2 migration guidance](https://docs.bitquery.io/docs/API-Blog/migrate-v1-v2/)
11. [V1 GraphQL query structure](https://docs.bitquery.io/v1/docs/building-queries/basic-structure-of-a-query)
12. [V2 GraphQL query principles](https://docs.bitquery.io/docs/graphql/query/)
13. [Dataset selection: realtime, archive and combined](https://docs.bitquery.io/docs/graphql/dataset/options/)

Solana helper references:

14. [Solana overview](https://docs.bitquery.io/docs/blockchain/Solana/)
15. [Solana instructions](https://docs.bitquery.io/docs/blockchain/Solana/solana-instructions/)
16. [Solana transfers](https://docs.bitquery.io/docs/blockchain/Solana/solana-transfers/)
17. [Solana balance updates](https://docs.bitquery.io/docs/blockchain/Solana/solana-balance-updates/)
18. [Solana transactions](https://docs.bitquery.io/docs/blockchain/Solana/solana-transactions/)

Useful per-helper source links live in [`v1/queries.go`](../v1/queries.go) and [`v2/queries.go`](../v2/queries.go). The Bitquery docs and IDE, not this module, remain the source of truth for live schema availability.
