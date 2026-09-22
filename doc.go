// Package bitquery is a Go SDK for the Bitquery blockchain data APIs.
//
// Bitquery exposes two deliberately separate API contracts:
//
//   - V1 — historical GraphQL over HTTPS (see package v1)
//   - V2 — streaming GraphQL over HTTPS plus WebSocket subscriptions
//     (see packages v2 and subscription)
//
// The two versions are NOT interchangeable: different schemas, endpoints
// and capabilities. This SDK never rewrites documents, substitutes
// endpoints or falls back between them — the version is always chosen
// explicitly by the caller via the v1/v2 packages.
//
// Authentication: OAuth access tokens are sent as
// "Authorization: Bearer <token>" for HTTP and as the "?token=" URL query
// parameter for WebSocket connections (the only mechanism Bitquery
// accepts for WSS). Credentials are redacted from all errors and logs.
//
// Numeric precision: response Data is exposed as json.RawMessage and
// helper decoding uses json.Number — token amounts and large integers
// are never silently converted to float64.
//
// Docs: https://docs.bitquery.io/docs/start/endpoints/
package bitquery
