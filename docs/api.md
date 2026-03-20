# Stamper API

## Health

### GET `/v1/health`

Response:

```json
{
  "status": "ok"
}
```

## Readiness

### GET `/v1/ready`

Response:

```json
{
  "ready": true,
  "policy_bundle_version": "v1"
}
```

## Create Run

### POST `/v1/runs`

Request:

```json
{
  "agent_id": "hermes-ops-agent",
  "environment": "prod",
  "task": "Fetch customer data"
}
```

Response:

```json
{
  "run_id": "run_123"
}
```

## Append Event

### POST `/v1/runs/{run_id}/events`

Request:

```json
{
  "event_type": "tool_requested",
  "payload": {
    "tool_name": "governed_http_request"
  }
}
```

## Finish Run

### POST `/v1/runs/{run_id}/finish`

Request:

```json
{
  "status": "failed",
  "output_summary": "Blocked by policy"
}
```

## Get Runs

### GET `/v1/runs`

## Get Run Detail

### GET `/v1/runs/{run_id}`

Returns:

- run metadata
- ordered events

## Evaluate Action

### POST `/v1/evaluate-action`

Request:

```json
{
  "run_id": "run_123",
  "agent": { "id": "hermes-ops-agent" },
  "environment": { "name": "prod" },
  "action": {
    "type": "tool_call",
    "tool_name": "governed_http_request",
    "arguments": {
      "url": "https://api.external.com"
    }
  }
}
```

Response:

```json
{
  "decision": "deny",
  "policy_id": "POL-NET-001",
  "rationale": "Outbound HTTP requests are not allowed in prod.",
  "reason": "Outbound HTTP requests are not allowed in prod."
}
```
