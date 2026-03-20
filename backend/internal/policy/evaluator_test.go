package policy

import "testing"

func TestEvaluatorNoMatchDefaultsToAllow(t *testing.T) {
	evaluator := newTestEvaluator(t, []Policy{
		{
			ID:        "POL-001",
			Name:      "Prod only",
			Enabled:   true,
			Priority:  100,
			Effect:    "deny",
			Rationale: "Prod tool calls are denied.",
			Scope: Scope{
				Agents:       []string{"*"},
				Environments: []string{"prod"},
				Teams:        []string{"*"},
			},
			Match: Match{
				ActionTypes: []string{"tool_call"},
				ToolNames:   []string{"http_request"},
			},
		},
	})

	decision, err := evaluator.Evaluate(ActionRequest{
		Agent:       Agent{ID: "agent-1", Team: "platform"},
		Environment: Environment{Name: "dev"},
		Action:      Action{Type: "tool_call", ToolName: "http_request"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if decision.Decision != "allow" {
		t.Fatalf("expected allow, got %q", decision.Decision)
	}
	if decision.PolicyID != "" {
		t.Fatalf("expected no policy id, got %q", decision.PolicyID)
	}
}

func TestEvaluatorDenyMatch(t *testing.T) {
	evaluator := newTestEvaluator(t, []Policy{
		{
			ID:        "POL-DENY-001",
			Name:      "Deny external HTTP",
			Enabled:   true,
			Priority:  200,
			Effect:    "deny",
			Rationale: "Outbound HTTP requests are not allowed in prod.",
			Scope: Scope{
				Agents:       []string{"*"},
				Environments: []string{"prod"},
				Teams:        []string{"*"},
			},
			Match: Match{
				ActionTypes: []string{"tool_call"},
				ToolNames:   []string{"http_request"},
				Conditions: []Condition{
					{
						Field:    "action.arguments.url",
						Operator: "contains",
						Value:    "external",
					},
				},
			},
		},
	})

	decision, err := evaluator.Evaluate(ActionRequest{
		Agent:       Agent{ID: "agent-1", Team: "platform"},
		Environment: Environment{Name: "prod"},
		Action: Action{
			Type:     "tool_call",
			ToolName: "http_request",
			Arguments: map[string]any{
				"url": "https://api.external.com",
			},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if decision.Decision != "deny" {
		t.Fatalf("expected deny, got %q", decision.Decision)
	}
	if decision.PolicyID != "POL-DENY-001" {
		t.Fatalf("expected policy id POL-DENY-001, got %q", decision.PolicyID)
	}
}

func TestEvaluatorRequireApprovalMatch(t *testing.T) {
	evaluator := newTestEvaluator(t, []Policy{
		{
			ID:        "POL-APPROVAL-001",
			Name:      "Approval for prod shell",
			Enabled:   true,
			Priority:  150,
			Effect:    "require_approval",
			Rationale: "Shell commands in prod require approval.",
			Scope: Scope{
				Agents:       []string{"*"},
				Environments: []string{"prod"},
				Teams:        []string{"platform"},
			},
			Match: Match{
				ActionTypes: []string{"tool_call"},
				ToolNames:   []string{"exec_command"},
			},
		},
	})

	decision, err := evaluator.Evaluate(ActionRequest{
		Agent:       Agent{ID: "agent-1", Team: "platform"},
		Environment: Environment{Name: "prod"},
		Action:      Action{Type: "tool_call", ToolName: "exec_command"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if decision.Decision != "require_approval" {
		t.Fatalf("expected require_approval, got %q", decision.Decision)
	}
	if !decision.ApprovalRequired {
		t.Fatal("expected approval_required=true")
	}
}

func TestEvaluatorHigherPriorityOverride(t *testing.T) {
	evaluator := newTestEvaluator(t, []Policy{
		{
			ID:        "POL-ALLOW-001",
			Name:      "Allow HTTP",
			Enabled:   true,
			Priority:  100,
			Effect:    "allow",
			Rationale: "HTTP is generally allowed.",
			Scope: Scope{
				Agents:       []string{"*"},
				Environments: []string{"prod"},
				Teams:        []string{"*"},
			},
			Match: Match{
				ActionTypes: []string{"tool_call"},
				ToolNames:   []string{"http_request"},
			},
		},
		{
			ID:        "POL-DENY-OVERRIDE-001",
			Name:      "Deny external HTTP in prod",
			Enabled:   true,
			Priority:  300,
			Effect:    "deny",
			Rationale: "External HTTP is denied in prod.",
			Scope: Scope{
				Agents:       []string{"*"},
				Environments: []string{"prod"},
				Teams:        []string{"*"},
			},
			Match: Match{
				ActionTypes: []string{"tool_call"},
				ToolNames:   []string{"http_request"},
				Conditions: []Condition{
					{
						Field:    "action.arguments.url",
						Operator: "contains",
						Value:    "external",
					},
				},
			},
		},
	})

	decision, err := evaluator.Evaluate(ActionRequest{
		Agent:       Agent{ID: "agent-1", Team: "platform"},
		Environment: Environment{Name: "prod"},
		Action: Action{
			Type:     "tool_call",
			ToolName: "http_request",
			Arguments: map[string]any{
				"url": "https://api.external.com",
			},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if decision.Decision != "deny" {
		t.Fatalf("expected deny, got %q", decision.Decision)
	}
	if decision.PolicyID != "POL-DENY-OVERRIDE-001" {
		t.Fatalf("expected high priority policy to win, got %q", decision.PolicyID)
	}
}

func newTestEvaluator(t *testing.T, policies []Policy) *Evaluator {
	t.Helper()

	evaluator, err := NewEvaluator(&Bundle{
		Version:  "v1",
		Policies: policies,
	})
	if err != nil {
		t.Fatalf("build evaluator: %v", err)
	}

	return evaluator
}
