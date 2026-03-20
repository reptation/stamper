# Stamper

**Policy enforcement and audit for AI agents**

Stamper is a sidecar service that evaluates, enforces, and records agent actions in real time.

It ensures that:
- unsafe actions are blocked before execution
- every decision is explainable
- every run is fully auditable

---

## Why Stamper?

AI agents are powerful—but in production, they need guardrails.

Without enforcement, agents can:
- call external APIs without restriction
- access sensitive data
- perform unintended actions
- behave unpredictably across environments

Stamper introduces a simple model:

> Every action must be evaluated before it executes.

---

## What It Does

Stamper sits alongside an agent and acts as a **policy decision point** and **audit system**.

### Core capabilities

- **Policy enforcement**
  - allow / deny / require approval
  - environment-aware (e.g. prod vs dev)
  - tool-aware (e.g. http_request)

- **Deterministic evaluation**
  - in-memory policy engine
  - no DB reads on hot path
  - priority-based rule matching

- **Structured audit trail**
  - full run lifecycle
  - ordered event timeline
  - human-readable decisions

---

## Example: Deny Path

A mock agent attempts to call an external API in production.  
Stamper evaluates the action and blocks it.

### Timeline

1. reasoning → "Need to fetch customer data"
2. tool_call → http_request
3. policy_decision → deny (POL-NET-001)
4. execution_blocked → blocked by policy
5. run_finished → failed

### Result

- the HTTP request never executes
- the decision is recorded with rationale
- the full trace is queryable via API

---

## Architecture

Agent Runtime  
│  
├── ActionRequest  
↓  
Stamper (sidecar)  
│  
├── allow / deny / require_approval  
↓  
Execution (only if allowed)

Stamper acts as:

- **Policy Decision Point (PDP)** — evaluates actions  
- **Audit system** — records every event in a run  

---

## Quick Start

### 1. Start Stamper

```bash
cd backend
go run ./cmd/stamperd
```

### 2. Run the demo agent

```bash
cd backend
go run ./cmd/demo
```

Example output:

```bash
run_id=run_960961404dac8d4c decision=deny policy_id=POL-NET-001
```

### 3. Inspect runs

```bash
curl http://127.0.0.1:8080/v1/runs
curl http://127.0.0.1:8080/v1/runs/<run_id>
```

---

## API

Core endpoints:

- `POST /v1/runs`
- `POST /v1/runs/{run_id}/events`
- `POST /v1/runs/{run_id}/finish`
- `GET /v1/runs`
- `GET /v1/runs/{run_id}`

See `docs/api.md` for full details.

---

## Project Structure

```text
backend/
  cmd/
    stamperd/     # sidecar service
    demo/         # mock governed agent
  internal/
    policy/       # in-memory evaluator
    runs/         # run + event storage
    httpapi/      # API handlers

docs/
  architecture.md
  policy-engine.md
  api.md
```

---

## Current Status

MVP features implemented:

- policy evaluator (allow / deny / require_approval)
- run + event storage (SQLite)
- HTTP API for runs and events
- end-to-end deny-path demo
- full audit timeline

---

## Roadmap

Next steps:

- Vue UI for run inspection
- approval workflows
- richer policy conditions
- real agent integrations (Hermes, etc.)
- policy management UI
