# Repository workflow

- Linux is the runtime and deployment target. Do not add Windows compatibility work or Windows-specific validation for new changes.
- Publish changes only to this EV repository; do not open pull requests or push branches to the upstream repository.

- Develop features, fixes, and documentation directly on `dev`. Fast-forward from the latest `origin/dev` before starting; do not create `codex/*` or other work branches for this repository.
- Sign and verify commits locally, run appropriate checks, and push them directly to `dev`. The `dev` ruleset requires verified signatures and prohibits force pushes and deletion; it does not require an inbound PR or PR-only status checks.
- Promote changes from `dev` to `main` through a pull request. Never push changes directly to `main`.
- `main` accepts only PRs from this repository's `dev`. Fork branches named `dev` and any other source branch cannot release to `main`.
- The required release checks are `PR policy tests` and `pr-route/main`. Keep route publication in the trusted default-branch workflow; never execute PR code in its write-enabled job.
- Keep `dev` and `main` after merges. Keep repository-wide automatic branch deletion disabled so release merges retain `dev`.
- Use Conventional Commits, sign commits with the configured GPG key, and verify signatures before pushing.
- Use SemVer release tag names (`vMAJOR.MINOR.PATCH`, with English prerelease suffixes when needed). Write annotated tag messages, Release titles, and Release notes in English.
- Maintain `CHANGELOG.md` in English. Release notes must record updates, optimizations, bug fixes, security fixes, hardening, validation, and upgrade/rollback considerations; explicitly state when a category has no changes. Identify the upstream baseline separately from the EV release version.
- Create signed annotated release tags only for the verified release commit on `main` after its `dev → main` PR passes the required checks and merges. Do not move existing published tags for documentation updates; record unreleased changes until the next version.
- Keep credentials, tokens, private host details, and runtime data out of tag annotations, Release notes, and changelog entries as well as commits and PRs. Follow the release-note structure in `docs/en-US/release-process.md`.
- Keep credentials, tokens, private environment details, and other sensitive information out of commit messages and pull request titles, descriptions, and comments.
- Preserve unrelated local changes. Do not rewrite published history or delete branches without explicit authorization.
- Run checks appropriate to the change and resolve required checks before merging. Follow [the contribution guide](docs/en-US/contributing-guide.md) and its [Chinese version](docs/zh-CN/contributing-guide.md).
