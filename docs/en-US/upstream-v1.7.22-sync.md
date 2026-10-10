# Upstream v1.7.22 synchronization

Upstream: [AIPentest/CyberStrikeAI v1.7.22](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.22), commit `762f798afbe4ea957e00fa4aedf1d5ceb762bdb6`.
Previous upstream baseline: `82b0af10f8519bf4baaa08958a8dbe38299a7685` (v1.7.21).

The release description highlights mobile navigation, drawers, gestures, responsive layouts, keyboard/safe-area handling, localization and deferred frontend dependencies. The actual baseline diff also includes configuration and WebShell credential masking, task cancellation/finalization, workflow validation/progress, knowledge indexing diagnostics, and C2 session authentication and task/file ownership checks.

## EV integration

- Preserve evidence-based vulnerability ratings, finding observations and project permission boundaries.
- Preserve synchronized storage policy snapshots, retention safeguards and the storage-specific configuration endpoint.
- Preserve SQLite immediate transactions and bounded lock recovery, gateway tool identities and audit parameters, and patched gRPC/image dependencies.
- Combine upstream role validation with EV role tool-policy preservation and validation.
- Retain EV's existing Nmap argument handling rather than reintroducing positional guessing; include upstream argument-order regressions.
- Retain the EV on-demand ELK loader; defer spreadsheet export dependency loading on mobile.
- Preserve pending severity styling alongside upstream UI additions.
- Normalize line endings in two upstream source-extraction test fixtures.

## Compatibility and verification

The HTTP C2 authentication changes add `c2_http_session_auth` and require the updated session-token protocol. Existing clients should not be assumed compatible; validate the updated protocol in an authorized isolated environment before upgrading a deployment using this module. Back up the database, executable, configuration and matching frontend files before any deployment.

Frontend and routing regression command:

```sh
node --test tests/web/*.test.cjs web/static/js/*.test.cjs tests/ci/*.test.cjs
```

252 tests passed. The Linux CI job now runs `go test -p 2 ./... -count=1 -timeout 5m` and `go build -mod=readonly -o /tmp/cyberstrike-ai ./cmd/server`. CI results on the PR determine Linux validation status. A local CGO-disabled cross-build is compilation-only and is not a deployable SQLite runtime validation.

No upstream repository changes, upstream PR, deployment, tag or Release publication are part of this synchronization. Actual mobile device/browser interaction and live provider integration are not covered by the automated checks.
