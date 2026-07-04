# Skill: web-search

## Purpose

Plan a safe web search for current or externally verifiable information.

## When to Use

Use this skill when the user asks for `web_search`, current information,
recent changes, prices, releases, rules, or facts that may have changed.

## Required Context

- The exact question to verify
- The required freshness or date range
- Any preferred source type or domain
- Whether a web search capability is available in the current runtime

## Procedure

1. Restate the search target as a concise query.
2. Prefer primary or official sources when they exist.
3. Check dates on search results before using them.
4. Compare at least two credible sources for unstable claims.
5. Summarize only what the sources support.
6. Include source names or links when the response depends on external facts.
7. If web access is unavailable, say that live verification cannot be performed.

## Related Tools

(None — this skill is procedural and does not execute search itself.)

## Verification

A web search answer is acceptable when:

- The answer distinguishes verified facts from inference.
- Time-sensitive claims include dates or source context.
- Unsupported or stale claims are not presented as current truth.

## Pitfalls

- Do not invent sources.
- Do not treat old search results as current.
- Do not rely on snippets when the source page should be checked.
