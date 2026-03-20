# Policy Engine

## Overview

The Stamper policy engine evaluates agent actions against a set of policies and returns a decision:

- allow
- deny
- require_approval

---

## ActionRequest Schema

```json
{
  "run_id": "run_123",
  "agent": {
    "id": "hermes-ops-agent",
    "team": "platform"
  },
  "environment": {
    "name": "prod"
  },
  "action": {
    "type": "tool_call",
    "tool_name": "http_request",
    "arguments": {
      "url": "https://api.external.com"
    }
  }
}
```

## Policy Schema

```json
{
  "id": "POL-NET-001",
  "name": "Block outbound HTTP in prod",
  "enabled": true,
  "priority": 200,
  "effect": "deny",
  "scope": {
    "agents": ["*"],
    "environments": ["prod"],
    "teams": ["*"]
  },
  "match": {
    "action_types": ["tool_call"],
    "tool_names": ["http_request"],
    "conditions": []
  }
}
```

## Decision Schema

```json
{
  "decision": "deny",
  "policy_id": "POL-NET-001",
  "policy_name": "Block outbound HTTP in prod",
  "rationale": "Outbound HTTP requests are not allowed in prod.",
  "approval_required": false
}
```

## Evaluation Algorithm

1. Filter enabled policies.
2. Match scope:
   - agent
   - environment
   - team
3. Match action:
   - action type
   - tool name
4. Evaluate conditions (all must pass).
5. Sort by priority (descending).
6. First match wins.

## Default Behavior

If no policy matches, `decision = allow`.

This will be configurable later.

## Supported Effects

- allow
- deny
- require_approval

## Conditions (MVP)

Conditions are optional.

Supported operators (initial set):

- equals
- not_equals
- in
- not_in
- contains

Example:

```json
{
  "field": "action.arguments.url",
  "operator": "contains",
  "value": "external"
}
```

## Startup Behavior

- policies loaded from local JSON file
- validated before activation
- compiled into memory

Failure:

- service not ready
- system fails closed

## Explainability

Every decision must include:

- `policy_id`
- `policy_name`
- `rationale`

Example:

> Denied by `POL-NET-001` because outbound HTTP is not allowed in prod.
