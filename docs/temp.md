## What does this PR do?

This PR updates Hermes' `governed_http_request` tool to use `stamper-proxy` as the enforcement point for outbound HTTP instead of executing approved requests directly from the Hermes process.

Before this change, the tool:

- called Stamper `/v1/evaluate-action`
- if allowed, executed the outbound request directly in Hermes

After this change, the tool:

- still asks Stamper for a policy decision
- requires an `approval_token` on allow decisions
- sends the request to `stamper-proxy`
- fails closed if the proxy is unavailable, the approval token is missing, or the proxy returns an invalid response

This strengthens the security model by moving request execution to a dedicated enforcement hop instead of trusting the Hermes process to perform the approved request itself.

## Related Issue

Fixes `#<issue-number>`

## Type of Change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [x] New feature (non-breaking change that adds functionality)
- [x] Security fix
- [ ] Documentation update
- [x] Tests (adding or improving test coverage)
- [ ] Refactor (no behavior change)
- [ ] New skill (bundled or hub)

## Changes Made

- Added `STAMPER_PROXY_URL` config entry in `hermes_cli/config.py` so the governed HTTP tool can require and discover the local proxy service.
- Updated `tools/governed_http_request_tool.py` so `check_governed_http_request_requirements()` now requires both `STAMPER_BASE_URL` and `STAMPER_PROXY_URL`.
- Added a new `ProxyClient` in `tools/governed_http_request_tool.py` that:
  - calls `POST {STAMPER_PROXY_URL}/request`
  - sends the request payload as JSON
  - sends the approval token in `X-Stamper-Token`
  - validates the proxy response shape before returning it to the tool handler
- Removed the direct HTTP execution path from the governed tool and replaced it with proxy-mediated execution after a Stamper allow decision.
- Added fail-closed handling for missing approval tokens and proxy failures.
- Updated the tool description/schema to reflect that execution now depends on both Stamper approval and `stamper-proxy` enforcement.
- Expanded tests in `tests/tools/test_governed_http_request_tool.py` to cover:
  - missing proxy configuration
  - allow path through proxy
  - proxy request payload expectations
  - proxy failure path
  - missing approval token fail-closed behavior

## How to Test

1. Start Stamper locally and ensure it is configured to return an allow decision with an `approval_token` for an approved request.
2. Start `stamper-proxy` locally and set:

```bash
export STAMPER_BASE_URL=http://localhost:8080
export STAMPER_PROXY_URL=http://localhost:8081
export STAMPER_TIMEOUT_MS=2000
```

3. Run Hermes and trigger `governed_http_request` with an allowed URL. Verify the tool:
   - calls Stamper for policy evaluation
   - sends the approved request to `http://localhost:8081/request`
   - includes `X-Stamper-Token`
   - returns the proxy response to the agent
4. Trigger a denied request and verify no outbound execution occurs.
5. Trigger an allow response without `approval_token` and verify the tool fails closed.
6. Stop `stamper-proxy` and verify the tool returns an error rather than falling back to direct HTTP execution.

## Checklist

### Code

- [ ] I've read the [Contributing Guide](https://github.com/NousResearch/hermes-agent/blob/main/CONTRIBUTING.md)
- [ ] My commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`fix(scope):`, `feat(scope):`, etc.)
- [ ] I searched for [existing PRs](https://github.com/NousResearch/hermes-agent/pulls) to make sure this isn't a duplicate
- [ ] My PR contains only changes related to this fix/feature (no unrelated commits)
- [ ] I've run `pytest tests/ -q` and all tests pass
- [ ] I've added tests for my changes (required for bug fixes, strongly encouraged for features)
- [ ] I've tested on my platform: `<your platform here>`

### Documentation & Housekeeping

- [ ] I've updated relevant documentation (`README`, `docs/`, docstrings) -- or N/A
- [ ] I've updated `cli-config.yaml.example` if I added or changed config keys -- or N/A
- [ ] I've updated `CONTRIBUTING.md` or `AGENTS.md` if I changed architecture or workflows -- or N/A
- [ ] I've considered cross-platform impact (Windows, macOS) per the [compatibility guide](https://github.com/NousResearch/hermes-agent/blob/main/CONTRIBUTING.md#cross-platform-compatibility) -- or N/A
- [ ] I've updated tool descriptions/schemas if I changed tool behavior -- or N/A

## Screenshots / Logs

Suggested logs to include:

- allow decision from Stamper including `approval_token`
- proxy request hitting `POST /request`
- denied / missing-token / proxy-down fail-closed result
