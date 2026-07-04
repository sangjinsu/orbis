# Skill: test-plan

## Purpose

Create a focused verification plan for Orbis changes before claiming completion.

## When to Use

Use this skill when the user asks for a test plan, verification steps, smoke
test, acceptance criteria, or proof that a change works.

## Required Context

- The behavior being changed
- Files or packages touched
- Relevant runtime path: reducer, worker, gateway, store, skill, or docs
- Manual behavior that automated tests cannot fully cover

## Procedure

1. Identify the smallest automated test that proves the change.
2. Add package-level tests when behavior changes.
3. Run the package test before the full suite.
4. Run `go test ./...` before completion.
5. Run `git diff --check` to catch whitespace issues.
6. Add a manual WebSocket or HTTP smoke test when user-facing runtime behavior changes.
7. Report exact commands and whether each passed or failed.

## Related Tools

(None — this skill is procedural and does not execute tests itself.)

## Verification

A test plan is sufficient when:

- It proves the requested behavior directly.
- It covers the most likely regression path.
- It includes manual steps for behavior that needs a running server.

## Pitfalls

- Do not claim completion without fresh command output.
- Do not use only broad tests when a narrow package test can catch the issue.
- Do not skip manual smoke tests for WebSocket-visible behavior changes.
