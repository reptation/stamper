# Security Model

## Overview

Stamper provides **policy enforcement and audit for AI agent actions**.

Its security model is based on **layered control**:

1. Governed tool access (application layer)
2. Policy decision (Stamper)
3. Mediated execution (proxy / sidecar)
4. Optional network-level enforcement (egress restriction)

These layers work together to reduce the risk of unintended or unauthorized agent behavior.

---

## Core Principles

### 1. No implicit trust in agent behavior

Agents are not assumed to behave safely.

All meaningful actions must be:
- explicitly invoked
- evaluated by policy
- recorded

---

### 2. All sensitive actions are mediated

Actions such as outbound HTTP must pass through a governed path.

There is no assumption that:
- prompts are sufficient
- agents will voluntarily follow rules

---

### 3. Enforcement is separate from decision

- **Stamper** decides whether an action is allowed
- **Execution layer (tool / proxy)** enforces that decision

This separation improves:
- clarity
- auditability
- composability

---

### 4. Audit is first-class

Every governed action produces a structured event trail:

- reasoning
- tool_call
- policy_decision
- execution outcome
- run status

This enables:
- debugging
- compliance review
- post-incident analysis

---

## Security Layers

### Layer 1 — Governed Toolset

Agents operate within a curated set of tools.

- Only approved tools are exposed
- Raw network-capable tools are excluded where governance is required
- Environment-specific toolsets may be used (e.g. dev vs prod)

**Goal:**
Limit the agent’s available action surface.

---

### Layer 2 — Policy Enforcement (Stamper)

Before execution, actions are evaluated by Stamper.

Inputs:
- agent identity
- environment
- action type
- tool arguments

Output:
- `allow`
- `deny`
- `require_approval`

**Goal:**
Make consistent, centralized decisions about agent behavior.

---

### Layer 3 — Mediated Execution (Stamper-proxy)

When enforcement is required, execution is routed through a controlled path.

Typical flow:

1. Tool constructs an action request
2. Stamper evaluates and (if allowed) issues a short-lived authorization token
3. Request is sent to `stamper-proxy`
4. Proxy validates the token
5. Proxy forwards the request only if valid

If:
- token is missing
- token is invalid
- request does not match authorized parameters

→ the request is denied

**Goal:**
Ensure only authorized actions are executed.

---

### Layer 4 — Network-Level Egress Restriction (Optional)

In stricter deployments, direct outbound traffic from the agent is restricted.

- Agent cannot make arbitrary external connections
- Only the proxy or approved endpoints are reachable
- Direct egress is not treated as a trusted path

**Goal:**
Prevent bypass of the governed execution path.

---

## Deployment Modes

### Governed Toolset Mode

- Agents use curated toolsets
- Governed tools call Stamper before execution
- No proxy enforcement required

**Properties:**
- Low friction
- Suitable for development and early adoption
- Relies on tool discipline

---

### Enforced Egress Mode

- Governed toolset mode plus:
- Execution must pass through `stamper-proxy`
- Direct outbound egress is restricted or unavailable

**Properties:**
- Stronger enforcement
- Reduced bypass risk
- Suitable for production / regulated environments

---

## Trust Assumptions

The security model assumes:

1. The agent runtime does not expose unrestricted network capabilities
2. The toolset is correctly configured for the environment
3. Governed tools correctly invoke Stamper before execution
4. In enforced mode, network egress is restricted to mediated paths

Violations of these assumptions may weaken enforcement guarantees.

---

## What This Model Protects Against

- Accidental or unintended outbound HTTP calls
- Policy violations due to agent reasoning errors
- Inconsistent enforcement across agents or environments
- Lack of auditability for agent actions

---

## What This Model Does Not Guarantee

- Full containment of a compromised runtime
- Protection against malicious code execution if unrestricted tools are exposed
- Deep inspection of encrypted payload contents
- Complete prevention of all possible bypasses in weak deployment configurations

---

## Summary

Stamper provides:

- **Governed action surfaces** (tool layer)
- **Centralized policy decisions** (Stamper)
- **Enforced execution paths** (proxy)
- **Structured audit trails** (runs + events)

In stronger deployments, it can enforce:

> **No external action occurs without explicit policy authorization.**

---

## Future Enhancements

Potential areas for strengthening the model:

- Capability-based authorization tokens
- Fine-grained request binding (headers, body hashes)
- Approval workflows
- Richer policy conditions
- Deeper network enforcement (eBPF, service mesh integration)
- Multi-agent isolation and attribution

---