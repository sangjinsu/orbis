# Reference: Go Concurrency

## Why This Matters

Orbis must be Go-native, not a literal Node.js clone.

## Key Ideas

- goroutines handle independent concurrent work
- per-session lanes preserve ordered state mutation
- `context.Context` controls cancellation and timeout
- channels carry event and worker result handoff

## Concepts to Borrow

- one logical actor per session
- worker boundaries for side effects
- non-blocking broker publish for slow subscribers

## Concepts Not to Borrow Yet

- distributed worker pools
- external message brokers
- complex dependency injection frameworks
