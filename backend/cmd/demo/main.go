package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/reptation/stamper/backend/internal/config"
	"github.com/reptation/stamper/backend/internal/policy"
)

const (
	mockAgentID   = "mock-agent"
	environment   = "prod"
	taskSummary   = "Fetch customer data from external API"
	requestURL    = "https://api.external-vendor.com/customers"
	requestMethod = "GET"
)

type demoResult struct {
	RunID    string
	Decision policy.Decision
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "demo: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	bundle, err := policy.LoadBundle(cfg.PolicyBundlePath)
	if err != nil {
		return fmt.Errorf("load policy bundle: %w", err)
	}

	evaluator, err := policy.NewEvaluator(bundle)
	if err != nil {
		return fmt.Errorf("build evaluator: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := runDemo(ctx, &http.Client{Timeout: 5 * time.Second}, apiBaseURL(cfg.HTTPAddr), evaluator)
	if err != nil {
		return err
	}

	fmt.Printf("run_id=%s decision=%s policy_id=%s\n", result.RunID, result.Decision.Decision, result.Decision.PolicyID)
	return nil
}

func runDemo(ctx context.Context, client *http.Client, baseURL string, evaluator *policy.Evaluator) (demoResult, error) {
	runID, err := createRun(ctx, client, baseURL)
	if err != nil {
		return demoResult{}, err
	}

	if err := appendEvent(ctx, client, baseURL, runID, "reasoning", map[string]any{
		"summary": "Need to fetch customer data from external API",
	}); err != nil {
		return demoResult{}, err
	}

	actionArguments := map[string]any{
		"url":    requestURL,
		"method": requestMethod,
	}
	if err := appendEvent(ctx, client, baseURL, runID, "tool_call", map[string]any{
		"tool_name": "http_request",
		"arguments": actionArguments,
	}); err != nil {
		return demoResult{}, err
	}

	decision, err := evaluator.Evaluate(policy.ActionRequest{
		RunID: runID,
		Agent: policy.Agent{
			ID: mockAgentID,
		},
		Environment: policy.Environment{
			Name: environment,
		},
		Action: policy.Action{
			Type:     "tool_call",
			ToolName: "http_request",
			Arguments: map[string]any{
				"url": requestURL,
			},
		},
	})
	if err != nil {
		return demoResult{}, fmt.Errorf("evaluate policy: %w", err)
	}
	if decision.Decision != "deny" {
		return demoResult{}, fmt.Errorf("expected deny decision, got %q", decision.Decision)
	}

	if err := appendEvent(ctx, client, baseURL, runID, "policy_decision", map[string]any{
		"decision":  decision.Decision,
		"policy_id": decision.PolicyID,
		"rationale": decision.Rationale,
	}); err != nil {
		return demoResult{}, err
	}

	if err := appendEvent(ctx, client, baseURL, runID, "execution_blocked", map[string]any{
		"reason": "Blocked by policy",
	}); err != nil {
		return demoResult{}, err
	}

	if err := finishRun(ctx, client, baseURL, runID, "failed", "Blocked by policy"); err != nil {
		return demoResult{}, err
	}

	return demoResult{
		RunID:    runID,
		Decision: decision,
	}, nil
}

func createRun(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	var response struct {
		RunID string `json:"run_id"`
	}

	err := postJSON(ctx, client, baseURL+"/v1/runs", map[string]string{
		"agent_id":    mockAgentID,
		"environment": environment,
		"task":        taskSummary,
	}, http.StatusCreated, &response)
	if err != nil {
		return "", err
	}

	if response.RunID == "" {
		return "", fmt.Errorf("create run: missing run_id in response")
	}

	return response.RunID, nil
}

func appendEvent(ctx context.Context, client *http.Client, baseURL, runID, eventType string, payload map[string]any) error {
	return postJSON(
		ctx,
		client,
		baseURL+"/v1/runs/"+runID+"/events",
		map[string]any{
			"event_type": eventType,
			"payload":    payload,
		},
		http.StatusCreated,
		nil,
	)
}

func finishRun(ctx context.Context, client *http.Client, baseURL, runID, status, outputSummary string) error {
	var response struct {
		OK bool `json:"ok"`
	}

	err := postJSON(
		ctx,
		client,
		baseURL+"/v1/runs/"+runID+"/finish",
		map[string]string{
			"status":         status,
			"output_summary": outputSummary,
		},
		http.StatusOK,
		&response,
	)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("finish run: expected ok=true response")
	}

	return nil
}

func postJSON(ctx context.Context, client *http.Client, url string, requestBody any, wantStatus int, responseBody any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		var errorResponse map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err == nil && errorResponse["error"] != "" {
			return fmt.Errorf("post %s: unexpected status %d: %s", url, resp.StatusCode, errorResponse["error"])
		}
		return fmt.Errorf("post %s: unexpected status %d", url, resp.StatusCode)
	}

	if responseBody == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func apiBaseURL(httpAddr string) string {
	if strings.HasPrefix(httpAddr, "http://") || strings.HasPrefix(httpAddr, "https://") {
		return strings.TrimRight(httpAddr, "/")
	}

	if strings.HasPrefix(httpAddr, ":") {
		return "http://127.0.0.1" + httpAddr
	}

	host, port, err := net.SplitHostPort(httpAddr)
	if err == nil {
		if host == "" {
			host = "127.0.0.1"
		}
		return "http://" + net.JoinHostPort(host, port)
	}

	return "http://" + httpAddr
}
