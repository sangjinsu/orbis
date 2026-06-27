# Reference: Node.js Event Loop

## Why This Matters

Orbis uses the Node.js event loop as conceptual inspiration, not as an implementation model.

## Key Ideas

- separate event intake from side-effect execution
- process state transitions in deterministic order
- keep timers, pending callbacks, polling, and close behavior conceptually distinct

## Concepts to Borrow

- event loop ownership by the runtime
- clear phase responsibilities
- observable lifecycle events

## Concepts Not to Borrow Yet

- literal Node.js phase implementation
- JavaScript callback semantics
