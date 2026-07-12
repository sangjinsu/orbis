# Project Memory: Orbis Agent Runtime

## Identity

Orbis Agent Runtime is a Go-based event-loop-first runtime for long-running AI agents.

## Current Baseline

`main` has shipped v0.1 through v2.1. The current operational baseline includes:

- the runtime kernel, tool calling, cancellation, timeout, persistence, and observable WebSocket events
- deterministic skill selection and bounded context injection
- reviewable skill proposals with explicit approval, versioned promotion, audit, and named reviewer/admin roles
- the runtime debug visualizer
- Cobra commands for server and smoke operation, skills, proposals, the global feed, and interactive chat

## Core Principle

The runtime owns the loop. The LLM is only one worker in the loop.

## Current Architecture

Orbis is a modular monolith: one Go process with clear domain, runtime, worker,
gateway, store, broker, protocol, skill, auth, observability, and configuration
boundaries. It uses goroutines, channels, `select`, `context.Context`, session
lane ordering, worker-owned side effects, and file-based JSON/JSONL persistence.

## Next Milestone

No next product milestone has been selected. Candidate work is not a commitment
until an accepted spec and decision record define it.

## Current Non-Goals

- unreviewed automatic skill promotion or self-modification
- vector or semantic search
- subagents
- MCP integration
- multi-channel messenger gateways
- distributed brokers or workers
- Kubernetes deployment
- full OpenClaw or Hermes compatibility
