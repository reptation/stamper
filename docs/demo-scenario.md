# Demo Scenario

## Scenario: Outbound HTTP blocked in prod

### Setup
- environment: prod
- agent: hermes-ops-agent
- policy: deny outbound HTTP

---

## Flow

1. Agent starts run
2. Agent decides to call external API
3. Agent emits tool_requested for `governed_http_request`
4. Agent calls Stamper `/v1/evaluate-action`
5. Stamper returns: deny
6. Agent does not execute the outbound request
7. Run finishes as failed

---

## UI Output

Timeline:

1. reasoning
2. tool_requested (`governed_http_request`)
3. policy_decision (deny)
4. run_finished

---

## Key Point

The HTTP request **never executes**.

This demonstrates enforcement, not observability.

---

## Phase 2: Approval Flow

Change policy to:

```text
effect = require_approval
```

Flow:
- agent receives require_approval
- approval created
- user approves in UI
- agent retries
- execution succeeds
