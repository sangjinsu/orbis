# Task Memory

## Current Implementation Stack

- PR #1: project baseline instructions
- PR #2: Go module bootstrap and `.env` config loader
- PR #3: domain model and real LLM provider interface
- PR #4: reducer state transitions
- PR #5: modular monolith and Korean PR conventions
- PR #6: file store and memory queue
- PR #7: LLM dispatcher
- PR #8: session lane
- PR #9: WebSocket gateway
- PR #10: app server runtime wiring
- PR #11: WebSocket event broker

## Remaining v0.1 Work

- add tool worker and tool-call reducer path
- add timer worker and timeout/cancel wiring
- implement `run.status`, `run.cancel`, `events.list`, and `session.create`
- add replay from JSONL event logs
- add manual WebSocket client documentation
- verify with a real OpenAI API key from `.env`
