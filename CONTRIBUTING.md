# Contributing

Thanks for contributing to Dhwani.

## Commit Guidelines

- Keep each commit focused on one logical change.
- Keep staged insertions to `<= 300` per commit.
- Prefer small, reviewable commits over large batch commits.
- Keep commit wording clean and consistent.

Examples:

- `feat: add lyrics lookup by song id`
- `fix: handle missing track metadata`
- `test: cover starred album persistence`
- `docs: update download-on-star setup`

## Local Hook Setup

This repository includes a versioned pre-commit hook in `.githooks/pre-commit`.

One-time setup:

```bash
git config core.hooksPath .githooks
```

## Typical Workflow

```bash
git add <files>
git diff --cached --numstat
git commit -m "feat: ..."
```
