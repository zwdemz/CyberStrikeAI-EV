# Changelog

This file records changes to CyberStrikeAI-EV in English. EV release versions and upstream baselines are tracked separately. See the [release process](docs/en-US/release-process.md) for signing, publication, and release-note requirements.

## Unreleased

### Upstream Baseline

The current baseline remains [AIPentest/CyberStrikeAI v1.7.21](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.21), commit `82b0af10f8519bf4baaa08958a8dbe38299a7685`.

### Updates

- Allow GitHub-registered SSH signing keys for verified commits and annotated release tags while retaining OpenPGP support; distinguish SSH push authentication from signature verification.
- Document the EV fork's purpose, education/enterprise SRC workflows, API/JWT/schema checks, and focused remediation verification in both READMEs.
- Distinguish inherited platform capabilities from the restricted SRC tool scope, including the additional DNSLog MCP provider.
- Require English tag annotations, Release titles/notes, and changelog entries with explicit update, optimization, fix, hardening, validation, and migration sections.

### Optimizations

- Recover a bounded, payload-free index of completed conversation tool steps from the durable event timeline before each Eino model call, including after context reduction. Stop a run if a completed tool result cannot be persisted, so the next step does not silently proceed without a record.
- Load the latest 40 chat messages initially, with cursor-based older history, bounded rendering batches and an indexed ordering query; preserve complete histories for exports and model context.
- Defer the optional graph layout engine until a graph is opened; initialize the chat shell without waiting for locale data and load independent metadata concurrently.
- Skip offscreen message layout and avoid rebuilding the turn navigator on every streamed text update.
- Remove the default extra DNS/TCP/TLS diagnostic connection from HTTP requests; probes are now explicit opt-in.

### Bug Fixes

- Restore `supervisor` delegation by forwarding Eino's classic sub-agent registration into the AgenticMessage model agent. Reject missing transfer support or an empty supervisor roster explicitly, and use the actual `transfer_to_agent` tool name in supervisor instructions.
- Prevent parallel `create_asset` and `update_asset` calls from failing on immediate SQLite read-to-write lock upgrades: start primary-database transactions with `BEGIN IMMEDIATE`, use an eight-connection pool and a 10-second busy wait, and retry only SQLite BUSY errors with bounded, cancellable attempts. Preserve batch atomicity and asset access rules.
- Pin the optional Burp extension Gradle build to a JDK 11-compatible wrapper so CodeQL can resolve Java dependencies instead of inferring them after an incompatible Gradle download.
- Cancel stale conversation requests and bound history fetch/JSON time to 15 seconds; expose retry controls and keep failed rendering from leaving loading permanently active.
- Render messages independently of approval metadata while preserving approval readiness checks before sending.
- Keep cursor boundaries stable when messages share timestamps or new replies arrive; mark short-lived, memory-bounded network fallback snapshots visibly.
- Keep Nmap scan options separate from valued additional arguments, fixing corrupted `--host-timeout` values.
- Report HTTP failure type, stage, elapsed time and incomplete response progress instead of ambiguous timeout text.
- Reject invalid, non-positive and non-finite HTTP timeout values before connecting.
- Correct README clone instructions to use the EV repository's `main` branch and align the documented Go requirement with `go.mod`.
- Replace the upstream-targeting upgrade recommendation with an EV-specific controlled upgrade procedure.
- Correct the description of missing-tool handling so it does not imply restricted roles can bypass policy through Python fallback.

### Security Fixes

- Omit raw HTTP exception messages from failure diagnostics to avoid disclosing credentials or proxy URLs.
- Replace TLS-failure workaround advice with certificate and connection diagnostics; no automatic insecure fallback is introduced.

### Hardening

- Clarify the existing SRC execution limits, authorization requirements, sensitive-data boundaries, and limitations of prompt-only controls.
- Document immutable published tags and exclusion of sensitive environment information from public release metadata.

### Validation

Chat history changes pass Linux database, handler, security and role-policy tests and browser checks with 2,000 synthetic messages (40 initial, 80 after one older-page load, no duplicates). JavaScript tests cover cancellation, timeout, stale navigation, renderer failure recovery and lazy graph loading. These are controlled fixture results, not an end-to-end latency guarantee for every deployment.

Checked 8 documents and 133 local links with no broken targets or anchors. Markdown rendering, fenced blocks, bilingual content, documented Go version, and SRC role limits were checked against the repository. Additional checks cover the security executor, SRC role policies and configuration on Windows, plus Python regression tests with local slow-header and slow-body fixtures. No external scan target is contacted.

### Upgrade and Rollback

Rebuild and restart the server and update `tools/http-framework-test.yaml` together after preserving local customizations. Back up and restore both artifacts together for rollback. Startup adds the non-destructive `idx_messages_conversation_created` index; it may remain after rollback. No configuration reset or message deletion is required. Deploy frontend assets and the server from the same commit. Existing published tags remain unchanged; these changes are collected for the next version.

## [v1.7.21](https://github.com/zwdemz/CyberStrikeAI-EV/releases/tag/v1.7.21) - 2026-10-07

### Upstream Baseline

Synchronized [AIPentest/CyberStrikeAI v1.7.21](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.21), commit `82b0af10f8519bf4baaa08958a8dbe38299a7685`, through [PR #11](https://github.com/zwdemz/CyberStrikeAI-EV/pull/11). EV release commit: `846d1ae084c819f6ddf4c8dfc15c9e926d0e10b4`. [Compare with EV v1.7.20](https://github.com/zwdemz/CyberStrikeAI-EV/compare/v1.7.20...v1.7.21).

### Updates

- Add Russian localization, locale-aware dates, persisted language selection, and English fallback for missing Russian translations.
- Add storage usage reporting, cleanup previews, per-category retention settings, and confirmed cleanup operations.
- Preserve EV role policies, tool guards, tool readiness handling, DNSLog integration, and bounded summary recovery.

### Optimizations

- Preserve custom summary retry budgets while selecting the provider-appropriate output-limit parameter.
- Include upstream conversation cleanup and storage activity tracking improvements.

### Bug Fixes

- Avoid sending conflicting OpenAI-compatible output-limit parameters in summary requests.
- Keep the original summary budget when retrying after an explicit output-limit failure; never replace history with an incomplete summary.

### Security Fixes

- Upgrade MCP Go SDK to v1.4.1, gRPC to v1.83.1, and golang.org/x/image to v0.43.0, addressing applicable [MCP](https://pkg.go.dev/vuln/GO-2026-4773), [gRPC](https://pkg.go.dev/vuln/GO-2026-6348), and [image decoder](https://pkg.go.dev/vuln/GO-2026-5062) advisory ranges.
- Upgrade OpenTelemetry to v1.45.0, AWS EventStream to v1.7.8, and required Go extension/transitive dependencies to patched versions.

### Hardening

- Keep automatic storage cleanup disabled by default and require explicit confirmation for deletion.
- Integrate storage operations with platform permissions and audit logging while retaining EV tool-call restrictions.

### Validation

- Linux full Go test suite and server build passed.
- Windows Go packages passed; four temporary-executable permission failures passed when rerun from fixed output paths.
- 171 frontend tests and 10 PR routing tests passed; PR checks included Linux, Windows, macOS, and CodeQL.
- The 2026-10-07 govulncheck scan reported zero reachable vulnerabilities after dependency updates; dependency-level records without detected call paths remained. This is not a claim that the entire application is vulnerability-free.
- Signed source commit and release tag verified locally and by GitHub. Deployment smoke checks covered service availability, unauthenticated endpoint rejection, source/binary checksums, and database integrity. Real model calls were not exercised in the deployment smoke checks.

### Upgrade and Rollback

Upstream increased the default summary reserve from 8192 to 40960. Set `multi_agent.eino_middleware.summarization_output_reserve_tokens: 8192` explicitly to retain the previous default when appropriate for the provider; preserve existing valid explicit values. Automatic storage cleanup remains disabled by default.

Back up the executable, source, configuration, and a consistent database snapshot before upgrading. Preserve credentials and runtime data, and review changes to custom role/tool policies. Restore a compatible backup if rollback is needed, accounting for data written after deployment. See the [synchronization guide](docs/zh-CN/upstream-v1.7.21-sync.md) (Chinese).
