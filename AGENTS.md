# Repository workflow

- Use `dev` as the ongoing integration branch. Start each new change from the latest `origin/dev` on a separate `codex/<description>` branch.
- Submit feature, fix, and documentation changes through a pull request targeting `dev`. Do not push changes directly to `dev` or `main`.
- Promote releases from `dev` to `main` through a separate pull request.
- Use Conventional Commits, sign commits with the configured GPG key, and verify signatures before pushing.
- Keep credentials, tokens, private environment details, and other sensitive information out of commit messages and pull request titles, descriptions, and comments.
- Preserve unrelated local changes. Do not rewrite published history or delete branches without explicit authorization.
- Run checks appropriate to the change and resolve required checks before merging. Follow [the contribution guide](docs/en-US/contributing-guide.md) and its [Chinese version](docs/zh-CN/contributing-guide.md).
