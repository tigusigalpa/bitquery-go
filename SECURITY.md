# Security Policy

## Reporting a vulnerability

Please report security issues **privately** to `sovletig@gmail.com`
(do not open a public issue). Include the affected version and a
reproduction path where possible.

## Supported versions

| Version | Supported |
| --- | --- |
| 0.1.x | Yes (latest minor) |

## Credential handling model

- HTTP auth uses `Authorization: Bearer <token>`.
- WebSocket auth uses the `?token=` URL parameter — the **only**
  Bitquery-supported method per official docs. The token is never sent
  in WS headers.
- All SDK error messages, contexts and logger output pass through
  `internal/redact`, which masks `token=` params, `Bearer` values and
  `access_token`/`client_secret`/`refresh_token` JSON & form fields.
- Never commit `.env`, tokens, client secrets or OAuth responses — not
  even in tests or fixtures.
- `ClientCredentialsProvider` omits the OAuth response body from error
  text on rejection (it may echo sensitive detail upstream).

## Scope notes

- The SDK does not persist data, run a database/indexer, or expose a UI.
- TLS is mandatory in practice (wss/https); explicit `ws://`/`http://`
  overrides exist only for tests and private infrastructure.
