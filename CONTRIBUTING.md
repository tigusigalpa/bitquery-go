# Contributing

## Ground rules

- **V1 and V2 stay separate.** Never add code that rewrites a document
  for the other version, swaps endpoints, or falls back between
  contracts. Helpers may exist per-version only.
- **No network at init.** Constructors must never dial. Network happens
  inside `Execute`/`Subscribe` only.
- **No secrets.** No real credentials in code, tests, fixtures, docs or
  CI. All error/log paths must pass `internal/redact`.
- **Precision.** Never decode payload numbers into `float64` — keep
  `json.RawMessage`/`json.Number`.
- **Open network set.** `Network` stays a string type; do not add a
  closed enum of "supported" chains.

## Workflow

```bash
go build ./...
gofmt -w .          # CI rejects unformatted code
go vet ./...
go test ./...       # must pass with no network
go test -race ./... # race suite is required
golangci-lint run   # optional locally; pinned in CI
```

- Keep tests hermetic: `httptest` for HTTP, `WSConn` fakes for sockets.
- Live calls only behind `BITQUERY_LIVE=1` + a real token, and never in
  public CI (`live_test.go` skips by default).
- Update `CHANGELOG.md` and docs for user-visible changes.

## Commit style

Match the repo history: short imperative subject, focused diffs, no
drive-by refactors.
