# Skill: go-reducer-pattern

## Purpose

Explain how deterministic reducers should be implemented in Orbis without side
effects.

## When to Use

Use this skill when the user wants to implement or change a reducer, or asks how
runtime state transitions should be written.

## Required Context

- The current `SessionState` and the incoming `Event`
- The set of valid `Action` and derived `Event` outputs
- The run state machine

## Procedure

1. Treat the reducer as `Event + Current State => New State + Actions`.
2. Keep the function pure: no LLM calls, no tool calls, no I/O, no goroutines.
3. Inspect only the current state and the event.
4. Produce the next state deterministically.
5. Emit actions for workers to execute side effects.
6. Emit derived internal events when a transition implies follow-up facts.
7. Return the same result for the same input every time.

## Related Tools

(None — reducers do not call tools.)

## Verification

A reducer is correct when:

- It never performs side effects.
- The same `(state, event)` input always yields the same result.
- Side effects only happen later, in workers, via emitted actions.

## Pitfalls

- Do not call an LLM or a tool from the reducer.
- Do not start goroutines or write to the network/disk from the reducer.
- Do not depend on wall-clock time or randomness for control flow.
