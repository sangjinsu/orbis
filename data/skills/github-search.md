# Skill: github-search

## Purpose

Search GitHub repository, issue, pull request, and release information in a
structured way.

## When to Use

Use this skill when the user asks to search GitHub, inspect issues, inspect pull
requests, find repository history, check releases, or use `gh search`.

## Required Context

- Repository owner and name
- Search target: code, issues, pull requests, commits, releases, or actions
- State filters such as open, closed, merged, failed, or recent
- Any branch, tag, or author constraints

## Procedure

1. Resolve the repository from git remotes when the user does not specify one.
2. Choose the narrowest GitHub surface that answers the question.
3. Apply state, branch, date, label, or author filters before broad search.
4. Summarize IDs, titles, states, dates, and URLs for relevant results.
5. Separate facts found on GitHub from local repository observations.
6. If GitHub access is unavailable, state what could be checked locally instead.

## Related Tools

(None — this skill is procedural and does not call GitHub itself.)

## Verification

A GitHub search result is acceptable when:

- The repository scope is clear.
- Issue or PR state is not inferred from stale local branches.
- URLs or identifiers are included for follow-up.

## Pitfalls

- Do not assume a local branch means a PR exists.
- Do not confuse commits on a branch with merged commits on `main`.
- Do not omit state when reporting issues or pull requests.
