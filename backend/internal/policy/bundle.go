package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

var validEffects = []string{"allow", "deny", "require_approval"}

type Bundle struct {
	Version  string   `json:"version"`
	Policies []Policy `json:"policies"`
}

type Policy struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Effect    string `json:"effect"`
	Rationale string `json:"rationale"`
	Scope     Scope  `json:"scope"`
	Match     Match  `json:"match"`
}

type Scope struct {
	Agents       []string `json:"agents"`
	Environments []string `json:"environments"`
	Teams        []string `json:"teams"`
}

type Match struct {
	ActionTypes []string    `json:"action_types"`
	ToolNames   []string    `json:"tool_names"`
	Conditions  []Condition `json:"conditions"`
}

type Condition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

func LoadBundle(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy bundle: %w", err)
	}

	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("decode policy bundle: %w", err)
	}

	if err := bundle.Validate(); err != nil {
		return nil, err
	}

	return &bundle, nil
}

func (b Bundle) Validate() error {
	if b.Version == "" {
		return fmt.Errorf("validate policy bundle: version must not be empty")
	}

	if len(b.Policies) == 0 {
		return fmt.Errorf("validate policy bundle: at least one policy is required")
	}

	seenIDs := make(map[string]struct{}, len(b.Policies))
	for i, policy := range b.Policies {
		if policy.ID == "" {
			return fmt.Errorf("validate policy bundle: policy[%d] id must not be empty", i)
		}
		if _, exists := seenIDs[policy.ID]; exists {
			return fmt.Errorf("validate policy bundle: duplicate policy id %q", policy.ID)
		}
		seenIDs[policy.ID] = struct{}{}

		if policy.Name == "" {
			return fmt.Errorf("validate policy bundle: policy %q name must not be empty", policy.ID)
		}
		if !slices.Contains(validEffects, policy.Effect) {
			return fmt.Errorf("validate policy bundle: policy %q has unsupported effect %q", policy.ID, policy.Effect)
		}
		if policy.Rationale == "" {
			return fmt.Errorf("validate policy bundle: policy %q rationale must not be empty", policy.ID)
		}
	}

	return nil
}
