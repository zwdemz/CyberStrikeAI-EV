# Repository workflow

- Develop features, fixes, and documentation directly on `dev`. Fast-forward from the latest `origin/dev` before starting; do not create `codex/*` or other work branches for this repository.
- Sign and verify commits locally, run appropriate checks, and push them directly to `dev`. The `dev` ruleset requires verified signatures and prohibits force pushes and deletion; it does not require an inbound PR or PR-only status checks.
- Promote changes from `dev` to `main` through a pull request. Never push changes directly to `main`.
- `main` accepts only PRs from this repository's `dev`. Fork branches named `dev` and any other source branch cannot release to `main`.
- The required release checks are `PR policy tests` and `pr-route/main`. Keep route publication in the trusted default-branch workflow; never execute PR code in its write-enabled job.
- Keep `dev` and `main` after merges. Keep repository-wide automatic branch deletion disabled so release merges retain `dev`.
- Use Conventional Commits, sign commits with the configured GPG key, and verify signatures before pushing.
- Keep credentials, tokens, private environment details, and other sensitive information out of commit messages and pull request titles, descriptions, and comments.
- Preserve unrelated local changes. Do not rewrite published history or delete branches without explicit authorization.
- Run checks appropriate to the change and resolve required checks before merging. Follow [the contribution guide](docs/en-US/contributing-guide.md) and its [Chinese version](docs/zh-CN/contributing-guide.md).
