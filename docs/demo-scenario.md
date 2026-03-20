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
3. Agent emits tool_call event
4. Agent calls Stamper `/v1/evaluate-action`
5. Stamper returns: deny
6. Agent blocks execution
7. Agent records execution_blocked
8. Run finishes as failed

---

## UI Output

Timeline:

1. reasoning
2. tool_call (http_request)
3. policy_decision (deny)
4. execution_blocked
5. run_finished

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
