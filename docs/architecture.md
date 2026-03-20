# Stamper Architecture

## Overview

Stamper is an **agent governance sidecar** that enforces policies on agent actions at runtime.

It sits in the execution path and ensures that:

- agent actions are evaluated before execution
- unsafe actions are blocked or require approval
- all decisions and outcomes are recorded as structured audit events

---

## Core Principle

> Agents do not execute sensitive actions directly.  
> All governed actions must pass through Stamper.

---

## High-Level Architecture

```text
Agent Runtime
|
| (ActionRequest)
v
Stamper Sidecar (Policy Engine + Audit)
|
| (Decision: allow / deny / require_approval)
v
Execution (only if allowed)
```

---

## Key Components

### 1. Agent Runtime (Mock or Hermes)

Responsible for:
- planning actions
- calling Stamper before execution
- executing actions only if allowed
- recording events

---

### 2. Stamper Sidecar

The core service.

Responsibilities:
- load policy bundle at startup
- evaluate actions in-memory
- return decisions
- record audit events
- manage run lifecycle

---

### 3. Policy Engine

In-memory evaluator that:
- matches actions against policies
- applies priority ordering
- returns deterministic decisions

---

### 4. Run + Event Store

Stores:
- runs (agent executions)
- ordered events (timeline)

Used by:
- UI
- debugging
- audit

---

### 5. Vue UI

Provides:
- run list
- run timeline
- policy decisions
- (later) approvals

---

## Control Plane vs Data Plane

### Data Plane (MVP focus)
- action evaluation (`/v1/evaluate-action`)
- run + event recording
- low-latency decisions

### Control Plane (future)
- policy authoring
- policy distribution
- approval management
- multi-tenant config

---

## Policy Decision vs Enforcement

### Policy Decision Point (PDP)
- implemented in Stamper
- evaluates action
- returns decision

### Policy Enforcement Point (PEP)
- implemented in agent wrapper
- blocks execution unless allowed

---

## Startup Contract

Stamper must:

1. load policy bundle
2. validate schema
3. compile into memory

If any step fails:
- service is not ready
- agent must not run
- system fails closed

---

## Execution Flow (MVP)

1. agent starts run
2. agent emits reasoning event
3. agent attempts tool call
4. agent calls `/v1/evaluate-action`
5. Stamper evaluates policy
6. Stamper returns decision

### If deny:
- execution is blocked
- event recorded

### If allow:
- agent executes action
- result recorded

### If require_approval (v1.1):
- approval requested
- execution paused

---

## Non-Goals (MVP)

- policy refresh / hot reload
- distributed policy sync
- capability tokens
- deep OS/network enforcement
- multi-agent orchestration

---

## Design Principles

- **fail closed**
- **deterministic decisions**
- **in-memory evaluation (no DB on hot path)**
- **structured audit events**
- **explainability over magic**

+------------------+       +-------------------------+       +-------------------------+       +----------------------+
|  Agent Runtime   | ----> | Governed Tool / Adapter | ----> |       Stamperd          | ----> |    Stamper Proxy     |
| (Hermes, etc.)   | tool  | governed_http_request   |       |-------------------------|       |----------------------|
|                  | call  |                         |       | Policy Engine           |       | Validate token       |
| Curated toolset  |       | Normalize ActionRequest |       | Run + Event Store       |       | Enforce method/host  |
| No raw egress    |       | Create run + events     |       | Authorization Issuer    |       | Forward if allowed   |
+------------------+       +-------------------------+       +-------------------------+       +----------+-----------+
          \                                                                                                  |
           \                                                                                                 |
            \---------------------------- X No direct sensitive action path X -------------------------------|
                                                                                                             |
                                                                                                             v
                                                                                                  +----------------------+
                                                                                                  | External Service/API |
                                                                                                  +----------------------+

                                      +-----------------------------------------------------------+
                                      |                     Stamper UI                             |
                                      |  Run Explorer · Run Detail Timeline · Policy Decisions    |
                                      +-----------------------------------------------------------+s