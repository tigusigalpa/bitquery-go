# Error handling

All SDK failures surface as `*bitquery.Error` (or wrapped equivalents)
with a `Kind`, an HTTP `StatusCode` when applicable, and a sanitized
`Message`/`Context`. Sentinels exist for `errors.Is`.

## Kind taxonomy

| Kind | Sentinel | When | Temporary? |
| --- | --- | --- | --- |
| `KindTransport` | `ErrTransport` | network/dial/read failures, unexpected HTTP status | transient network errors: yes |
| `KindAuthentication` | `ErrAuthentication` | HTTP 401 (token refresh is attempted once first) | no — refresh token |
| `KindAuthorization` | `ErrAuthorization` | HTTP 403 | no |
| `KindPlanEntitlement` | `ErrPlanEntitlement` | HTTP 402 — plan does not cover the request | **no — never retried** |
| `KindRateLimited` | `ErrRateLimited` | HTTP 429 **or** documented "temporarily blocked" shared-compute body | yes — see `RetryAfter` |
| `KindServer` | `ErrServer` | HTTP 5xx | yes for 500/502/503/504 |
| `KindGraphQL` | `ErrGraphQL` | strict mode with non-empty `errors[]` | n/a — inspect `Response` |
| `KindSubscription` | `ErrSubscription` | WebSocket lifecycle (dial exhaustion, overflow-fail, connection_error) | per case |
| `KindConfig` | `ErrInvalidConfig` | missing provider, bad region/version, empty document | no |

```go
var be *bitquery.Error
if errors.As(err, &be) {
    be.Kind, be.StatusCode, be.RetryAfter, be.Temporary
    be.Response        // KindGraphQL: full response incl. partial data
    be.GraphQLErrors   // parsed errors[] entries
}
errors.Is(err, bitquery.ErrRateLimited)
bitquery.IsRetryable(err)
```

## GraphQL `errors[]` vs HTTP errors

Bitquery returns `errors[]` inside HTTP 200 responses — semantically
different from transport failures:

- **Tolerant mode (default):** `Execute` returns the `Response` with
  `resp.Errors` populated and `err == nil`. `resp.HasPartialData()`
  distinguishes data+errors from pure errors.
- **Strict mode** (`WithStrict()` or `ExecuteStrict`): any `errors[]`
  produces `*Error{Kind: KindGraphQL}` — and `be.Response` still holds
  the full response, partial data included.

## Retry semantics (defaults follow official guidance)

- `MaxAttempts: 4`, `BaseDelay: 5s`, `MaxDelay: 60s`, `Jitter: 0.25`.
- Retried only for read operations: 429, 500, 502, 503, 504, network
  errors, and the documented "temporarily blocked" shared-compute
  response (any status). Mutations and HTTP subscriptions are never
  replayed automatically.
- `Retry-After` (seconds or HTTP date) overrides the computed delay,
  capped at `MaxDelay`.
- Never retried: 400/401(after refresh)/402/403/404 — plan/auth
  problems are not transient.
- 401 → `TokenProvider.Refresh()` once, then re-run; second 401 →
  `KindAuthentication`.
- Subscribe-side reconnects share the same policy object; consecutive
  failures reset after an acknowledged connection.

## Redaction guarantee

Every `Message` and `Context` value passes `internal/redact`:
`token=` URL params, `Bearer` headers, `access_token`/`client_secret`
JSON/form fields are replaced with `[REDACTED]` — including error text
surfaced from the WebSocket dialer, which contains the auth URL.
