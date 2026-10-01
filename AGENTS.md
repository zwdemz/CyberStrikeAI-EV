# Repository workflow

- Use `dev` as the ongoing integration branch. Start each new change from the latest `origin/dev` on a separate `codex/<description>` branch.
- Submit feature, fix, and documentation changes through a pull request targeting `dev`. Do not push changes directly to `dev` or `main`.
- Promote releases from `dev` to `main` through a separate pull request.
- `main` accepts only PRs from this repository's `dev`; `codex/*`, `feature/*`, `fix/*` and other work branches must target `dev`. Fork branches named `dev` cannot release to `main`.
- The required checks are `PR policy tests` and `pr-route/dev` or `pr-route/main`, according to the target. Keep route publication in the trusted default-branch workflow; never execute PR code in its write-enabled job.
- Keep `dev` and `main` after merges. Delete merged work branches only after confirming their tips are contained in `dev` and obtaining the requested cleanup authorization.
- Use Conventional Commits, sign commits with the configured GPG key, and verify signatures before pushing.
- Keep credentials, tokens, private environment details, and other sensitive information out of commit messages and pull request titles, descriptions, and comments.
- Preserve unrelated local changes. Do not rewrite published history or delete branches without explicit authorization.
- Run checks appropriate to the change and resolve required checks before merging. Follow [the contribution guide](docs/en-US/contributing-guide.md) and its [Chinese version](docs/zh-CN/contributing-guide.md).
