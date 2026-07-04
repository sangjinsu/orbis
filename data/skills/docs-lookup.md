# Skill: docs-lookup

## Purpose

Look up official documentation before using SDKs, APIs, frameworks, CLIs, or
cloud services.

## When to Use

Use this skill when the user asks about docs, official docs, API docs, SDK
usage, CLI usage, setup, configuration, or version-specific behavior.

## Required Context

- Library, framework, API, CLI, or service name
- Version, if the project pins one
- The specific behavior or API contract being used
- Relevant local config or code references

## Procedure

1. Identify the exact product and version from local files when possible.
2. Prefer official documentation over blog posts or examples.
3. Verify method names, request fields, response fields, and configuration keys.
4. Check migration notes when behavior may be version-specific.
5. Apply only the documented contract to the implementation or explanation.
6. If official docs are unavailable, label any fallback source clearly.

## Related Tools

(None — this skill is procedural and does not fetch documentation itself.)

## Verification

Documentation lookup succeeds when:

- The answer cites the official contract or clearly states the fallback source.
- Version-sensitive behavior is tied to the version in use.
- Guessed field names or undocumented options are avoided.

## Pitfalls

- Do not copy patterns from old code without checking the current docs.
- Do not assume SDK defaults when the docs define explicit configuration.
- Do not mix examples from incompatible versions.
