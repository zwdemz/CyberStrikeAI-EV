# Release Process

[中文](../zh-CN/release-process.md)

Use this guide for maintainers and operators preparing upgrades or releases.

## EV Versioning and Publication Rules

- Maintain changes directly on `dev`; publish to `main` through a same-repository `dev → main` PR after the required checks pass. Never push directly to `main`.
- Use SemVer tag names such as `v1.7.21`, with English prerelease suffixes such as `-rc.1` when needed. EV versions may advance independently of upstream; always name the upstream repository, release, and commit separately.
- Create a GPG-signed annotated tag pointing to the verified release commit on `main`. Verify the commit and tag locally and check GitHub's verification status after pushing.
- Write tag annotations, Release titles, Release notes, and [`CHANGELOG.md`](../../CHANGELOG.md) in English. A title can be the version alone or the version plus an English summary; the tag annotation must summarize concrete changes and reference the corresponding changelog entry.
- Do not move or reuse a published tag for later fixes or documentation edits. Collect those changes under `Unreleased`, then publish a new version when ready. A documentation PR alone does not require a new tag.
- Keep credentials, private hosts, deployment paths, runtime logs, personal data, and other sensitive environment details out of public release metadata and artifacts.

## Required English Release-Note Sections

| Section | Required content |
| --- | --- |
| Upstream Baseline | Upstream release URL and commit, distinguished from the EV version and release commit. |
| Updates | Added features, behavior/configuration changes, and documentation updates. |
| Optimizations | Concrete reliability, performance, or usability improvements; give measurements only when measured. |
| Bug Fixes | Corrected behavior and the conditions that triggered the bug. |
| Security Fixes | Resolved vulnerabilities or affected dependencies, with advisory/fixed-version links where available. |
| Hardening | Changes to authorization, role restrictions, input validation, isolation, audit, or safe defaults. |
| Validation | Tests, builds, scans, tested platforms, and any checks or scenarios not verified. |
| Upgrade and Rollback | Compatibility changes, backups, migration steps, and rollback limitations. |

Keep every section; write `No changes in this release.` when a change category is empty. Validation must describe actual results, including limitations, rather than using a generic success claim. A clean dependency scan does not establish that the entire application is vulnerability-free. Link the relevant PRs and the comparison with the preceding EV release.

For a release: finalize the changelog on `dev`, merge its PR to `main`, verify the release commit and checks, sign and verify the tag, push it, and publish an English Release from that tag. Existing published tags remain immutable. This is a maintainer workflow; no automated language enforcement is currently installed.

## Pre-Release Checklist

- README and docs updated.
- `config.yaml` sample includes new fields.
- OpenAPI includes new endpoints.
- i18n updated when frontend text changed.
- Security docs updated for high-risk capabilities.

## Release Risk Tiers

| Change | Risk | Must test |
| --- | --- | --- |
| Docs/assets | low | links/rendering |
| Frontend | medium | login, page states, API errors |
| Handler/API | medium | OpenAPI, auth, errors |
| Config struct | high | old config compatibility, ApplyConfig |
| DB schema | high | old DB migration, rollback |
| Agent/MCP/HITL | high | tools, approvals, streaming |
| C2/WebShell/Terminal | critical | authorized lab, audit, disable switch |

Release notes should call out risk, not just features.

## Config Compatibility

New fields should:

- have safe defaults;
- allow old configs to start;
- be documented in sample `config.yaml`;
- not cause Web settings to delete unknown fields;
- be tested via restart and hot-apply paths.

Avoid default-enabling high-risk capabilities.

## Database Changes

SQLite migrations must be:

- compatible with old versions;
- idempotent after interruption;
- careful with nullable/default fields;
- mindful of large indexes and locks;
- documented with backup instructions.

## Build and Test

```bash
go test ./internal/...
go test ./cmd/...
go build -o cyberstrike-ai ./cmd/server
```

Manual smoke:

```text
login -> model test -> new chat -> tools -> HITL -> KB -> external MCP -> C2 enable/disable
```

## Rollback

Restore binary/code, `config.yaml`, and `data/` together. If a new version changed DB schema, replacing only the binary is not a reliable rollback.
