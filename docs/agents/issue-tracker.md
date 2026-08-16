# Issue tracker: GitHub

Issues and PRDs for this repository live as GitHub issues. Use the `gh` CLI for all operations.

## Repository

- GitHub repository: `EquentR/sub2api_giftcode`
- Default branch: `main`
- Infer the repository from the configured Git remote when running inside this clone.

## Conventions

- Create issues with `gh issue create`.
- Read issues and comments with `gh issue view <number> --comments`.
- List issues with `gh issue list`, requesting labels and comments as structured JSON when needed.
- Comment with `gh issue comment <number> --body "..."`.
- Apply or remove labels with `gh issue edit`.
- Close issues with `gh issue close` and include a closing comment when context is useful.

## Pull requests as a triage surface

**PRs as a request surface: no.**

External pull requests are not treated as feature requests by the triage workflow.

## Skill operations

- When a skill says to publish to the issue tracker, create a GitHub issue.
- When a skill says to fetch a ticket, read the corresponding GitHub issue and its comments.
- GitHub issues and pull requests share a number space; resolve ambiguous references before acting.

## Wayfinding

Wayfinding maps and child tickets use GitHub issues. Prefer native sub-issues and native issue dependencies when available; otherwise record child and blocking relationships explicitly in issue bodies.
