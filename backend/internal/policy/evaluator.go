package policy

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type Evaluator struct {
	policies []Policy
}

type ActionRequest struct {
	RunID       string         `json:"run_id"`
	Agent       Agent          `json:"agent"`
	Environment Environment    `json:"environment"`
	Action      Action         `json:"action"`
	Resource    map[string]any `json:"resource,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

type Agent struct {
	ID   string `json:"id"`
	Team string `json:"team"`
}

type Environment struct {
	Name string `json:"name"`
}

type Action struct {
	Type      string         `json:"type"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type Decision struct {
	Decision         string `json:"decision"`
	PolicyID         string `json:"policy_id,omitempty"`
	PolicyName       string `json:"policy_name,omitempty"`
	Rationale        string `json:"rationale"`
	ApprovalRequired bool   `json:"approval_required"`
}

func NewEvaluator(bundle *Bundle) (*Evaluator, error) {
	if bundle == nil {
		return nil, fmt.Errorf("build evaluator: bundle is required")
	}
	if err := bundle.Validate(); err != nil {
		return nil, err
	}

	policies := make([]Policy, 0, len(bundle.Policies))
	for _, policy := range bundle.Policies {
		if policy.Enabled {
			policies = append(policies, policy)
		}
	}

	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].Priority > policies[j].Priority
	})

	return &Evaluator{policies: policies}, nil
}

func (e *Evaluator) Evaluate(request ActionRequest) (Decision, error) {
	for _, policy := range e.policies {
		matched, err := matchesPolicy(policy, request)
		if err != nil {
			return Decision{}, err
		}
		if !matched {
			continue
		}

		return Decision{
			Decision:         policy.Effect,
			PolicyID:         policy.ID,
			PolicyName:       policy.Name,
			Rationale:        policy.Rationale,
			ApprovalRequired: policy.Effect == "require_approval",
		}, nil
	}

	return Decision{
		Decision:         "allow",
		Rationale:        "No policy matched; default allow.",
		ApprovalRequired: false,
	}, nil
}

func matchesPolicy(policy Policy, request ActionRequest) (bool, error) {
	if !matchesScope(policy.Scope, request) {
		return false, nil
	}
	if !matchesAction(policy.Match, request) {
		return false, nil
	}

	for _, condition := range policy.Match.Conditions {
		ok, err := evaluateCondition(condition, request)
		if err != nil {
			return false, fmt.Errorf("evaluate policy %q condition %q: %w", policy.ID, condition.Field, err)
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

func matchesScope(scope Scope, request ActionRequest) bool {
	return matchesStringList(scope.Agents, request.Agent.ID) &&
		matchesStringList(scope.Environments, request.Environment.Name) &&
		matchesStringList(scope.Teams, request.Agent.Team)
}

func matchesAction(match Match, request ActionRequest) bool {
	return matchesStringList(match.ActionTypes, request.Action.Type) &&
		matchesStringList(match.ToolNames, request.Action.ToolName)
}

func matchesStringList(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == "*" || value == actual {
			return true
		}
	}
	return false
}

func evaluateCondition(condition Condition, request ActionRequest) (bool, error) {
	actual, ok := lookupField(request, condition.Field)
	if !ok {
		return false, nil
	}

	switch condition.Operator {
	case "equals":
		return compareEqual(actual, condition.Value), nil
	case "not_equals":
		return !compareEqual(actual, condition.Value), nil
	case "contains":
		actualText, ok := actual.(string)
		if !ok {
			return false, nil
		}
		expectedText, ok := condition.Value.(string)
		if !ok {
			return false, fmt.Errorf("contains expects string value")
		}
		return strings.Contains(actualText, expectedText), nil
	case "in":
		return valueIn(actual, condition.Value), nil
	case "not_in":
		return !valueIn(actual, condition.Value), nil
	default:
		return false, fmt.Errorf("unsupported operator %q", condition.Operator)
	}
}

func lookupField(request ActionRequest, field string) (any, bool) {
	data := map[string]any{
		"run_id": request.RunID,
		"agent": map[string]any{
			"id":   request.Agent.ID,
			"team": request.Agent.Team,
		},
		"environment": map[string]any{
			"name": request.Environment.Name,
		},
		"action": map[string]any{
			"type":      request.Action.Type,
			"tool_name": request.Action.ToolName,
			"arguments": request.Action.Arguments,
		},
		"resource": request.Resource,
		"context":  request.Context,
	}

	current := any(data)
	for _, part := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := object[part]
		if !ok {
			return nil, false
		}
		current = next
	}

	return current, true
}

func compareEqual(actual, expected any) bool {
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		return false
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return false
	}

	return string(actualJSON) == string(expectedJSON)
}

func valueIn(actual, expected any) bool {
	switch typed := expected.(type) {
	case []any:
		for _, item := range typed {
			if compareEqual(actual, item) {
				return true
			}
		}
	case []string:
		actualText, ok := actual.(string)
		if !ok {
			return false
		}
		return slices.Contains(typed, actualText)
	}

	return false
}
