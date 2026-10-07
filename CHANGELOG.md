# Changelog

This file records changes to CyberStrikeAI-EV in English. EV release versions and upstream baselines are tracked separately. See the [release process](docs/en-US/release-process.md) for signing, publication, and release-note requirements.

## Unreleased

### Upstream Baseline

The current baseline remains [AIPentest/CyberStrikeAI v1.7.21](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.21), commit `82b0af10f8519bf4baaa08958a8dbe38299a7685`.

### Updates

- Document the EV fork's purpose, education/enterprise SRC workflows, API/JWT/schema checks, and focused remediation verification in both READMEs.
- Distinguish inherited platform capabilities from the restricted SRC tool scope, including the additional DNSLog MCP provider.
- Require English tag annotations, Release titles/notes, and changelog entries with explicit update, optimization, fix, hardening, validation, and migration sections.

### Optimizations

No runtime changes in this documentation update.

### Bug Fixes

- Correct README clone instructions to use the EV repository's `main` branch and align the documented Go requirement with `go.mod`.
- Replace the upstream-targeting upgrade recommendation with an EV-specific controlled upgrade procedure.
- Correct the description of missing-tool handling so it does not imply restricted roles can bypass policy through Python fallback.

### Security Fixes

No runtime security fixes in this documentation update.

### Hardening

- Clarify the existing SRC execution limits, authorization requirements, sensitive-data boundaries, and limitations of prompt-only controls.
- Document immutable published tags and exclusion of sensitive environment information from public release metadata.

### Validation

Checked 8 documents and 133 local links with no broken targets or anchors. Markdown rendering, fenced blocks, bilingual content, documented Go version, and SRC role limits were checked against the repository. Runtime code and configuration are unchanged.

### Upgrade and Rollback

No runtime migration is required. Existing published tags remain unchanged; documentation changes are collected here for the next version.

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
