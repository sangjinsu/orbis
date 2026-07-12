# Event Model

## Status

Accepted — historical baseline; the event rules remain current

## Purpose

Events are immutable facts that already happened. Reducers consume events and current state to produce next state and actions.

## Data Model

The domain event envelope is implemented in `internal/domain.Event`:

- `event_id`
- `session_id`
- `run_id`
- `type`
- `seq`
- `created_at`
- `payload`

## Rules

- Reducers must not execute side effects.
- Worker results must return as events.
- Events are appended to JSONL before reducer state transitions are saved.
