# Project Memory: Orbis Agent Runtime

## Identity

Orbis Agent Runtime is a Go-based event-loop-first runtime for long-running AI agents.

## Current Goal

Build the v0.1 runtime kernel with WebSocket-based real LLM communication testing.

## Core Principle

The runtime owns the loop. The LLM is only one worker in the loop.

## Architecture

Orbis uses a modular monolith architecture: one Go process with clear internal package boundaries.

## Implementation Direction

Use Go-native concurrency:

- goroutines
- channels
- `select`
- `context.Context`
- worker boundaries
- session lane ordering
- WebSocket event streams

## Current Non-Goals

- OpenClaw compatibility
- Hermes compatibility
- multi-channel messenger gateway
- skills
- tool search
- subagents
- durable task board
- distributed queue
