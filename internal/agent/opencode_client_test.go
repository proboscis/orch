package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNil  bool
		provider string
		model    string
	}{
		{
			name:     "valid anthropic model",
			input:    "anthropic/claude-opus-4-5",
			wantNil:  false,
			provider: "anthropic",
			model:    "claude-opus-4-5",
		},
		{
			name:     "valid openai model",
			input:    "openai/gpt-4",
			wantNil:  false,
			provider: "openai",
			model:    "gpt-4",
		},
		{
			name:     "model with multiple slashes",
			input:    "provider/model/version",
			wantNil:  false,
			provider: "provider",
			model:    "model/version",
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "no slash",
			input:   "claude-opus-4-5",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseModel(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Errorf("ParseModel(%q) = %+v, want nil", tt.input, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("ParseModel(%q) = nil, want non-nil", tt.input)
			}
			if result.ProviderID != tt.provider {
				t.Errorf("ProviderID = %q, want %q", result.ProviderID, tt.provider)
			}
			if result.ModelID != tt.model {
				t.Errorf("ModelID = %q, want %q", result.ModelID, tt.model)
			}
		})
	}
}

func TestPromptRequestJSON(t *testing.T) {
	// Test that variant is at top level, not inside model
	req := PromptRequest{
		Parts: []MessagePart{
			{Type: "text", Text: "Hello"},
		},
		Model: &ModelRef{
			ProviderID: "anthropic",
			ModelID:    "claude-opus-4-5",
		},
		Variant: "max",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Parse back to verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// Check variant is at top level
	if _, ok := parsed["variant"]; !ok {
		t.Error("variant should be at top level of JSON")
	}
	if parsed["variant"] != "max" {
		t.Errorf("variant = %v, want %q", parsed["variant"], "max")
	}

	// Check model structure
	model, ok := parsed["model"].(map[string]interface{})
	if !ok {
		t.Fatal("model should be an object")
	}
	if model["providerID"] != "anthropic" {
		t.Errorf("model.providerID = %v, want %q", model["providerID"], "anthropic")
	}
	if model["modelID"] != "claude-opus-4-5" {
		t.Errorf("model.modelID = %v, want %q", model["modelID"], "claude-opus-4-5")
	}
	// Verify variant is NOT inside model
	if _, ok := model["variant"]; ok {
		t.Error("variant should NOT be inside model object")
	}
}

func TestPromptRequestJSONWithoutVariant(t *testing.T) {
	req := PromptRequest{
		Parts: []MessagePart{
			{Type: "text", Text: "Hello"},
		},
		Model: &ModelRef{
			ProviderID: "anthropic",
			ModelID:    "claude-opus-4-5",
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Variant should be omitted when empty
	if strings.Contains(string(data), "variant") {
		t.Error("empty variant should be omitted from JSON")
	}
}

func TestCreateSessionWithDirectory(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()

		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Session{
			ID:    "ses_test123",
			Title: "Test Session",
		})
	}))
	defer server.Close()

	// Extract port from server URL
	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	session, err := client.CreateSession(ctx, "Test Session", "/path/to/worktree")
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	// Verify session was returned
	if session.ID != "ses_test123" {
		t.Errorf("session.ID = %q, want %q", session.ID, "ses_test123")
	}

	// Verify X-OpenCode-Directory header was sent
	dirHeader := receivedHeaders.Get("X-OpenCode-Directory")
	if dirHeader != "/path/to/worktree" {
		t.Errorf("X-OpenCode-Directory header = %q, want %q", dirHeader, "/path/to/worktree")
	}

	// Verify title in body
	if receivedBody["title"] != "Test Session" {
		t.Errorf("body.title = %v, want %q", receivedBody["title"], "Test Session")
	}
}

func TestSendMessageAsyncWithDirectoryAndModel(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody map[string]interface{}
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedPath = r.URL.Path

		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	model := &ModelRef{
		ProviderID: "anthropic",
		ModelID:    "claude-opus-4-5",
	}

	err := client.SendMessageAsync(ctx, "ses_test123", "Hello world", "/path/to/worktree", model, "max")
	if err != nil {
		t.Fatalf("SendMessageAsync error: %v", err)
	}

	// Verify path includes session ID
	expectedPath := "/session/ses_test123/prompt_async"
	if receivedPath != expectedPath {
		t.Errorf("request path = %q, want %q", receivedPath, expectedPath)
	}

	// Verify X-OpenCode-Directory header
	dirHeader := receivedHeaders.Get("X-OpenCode-Directory")
	if dirHeader != "/path/to/worktree" {
		t.Errorf("X-OpenCode-Directory header = %q, want %q", dirHeader, "/path/to/worktree")
	}

	// Verify Content-Type header
	contentType := receivedHeaders.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", contentType, "application/json")
	}

	// Verify body structure
	// Check parts
	parts, ok := receivedBody["parts"].([]interface{})
	if !ok || len(parts) != 1 {
		t.Fatalf("body.parts should be array with 1 element, got %v", receivedBody["parts"])
	}
	part := parts[0].(map[string]interface{})
	if part["type"] != "text" {
		t.Errorf("part.type = %v, want %q", part["type"], "text")
	}
	if part["text"] != "Hello world" {
		t.Errorf("part.text = %v, want %q", part["text"], "Hello world")
	}

	// Check model
	model2, ok := receivedBody["model"].(map[string]interface{})
	if !ok {
		t.Fatalf("body.model should be an object, got %v", receivedBody["model"])
	}
	if model2["providerID"] != "anthropic" {
		t.Errorf("model.providerID = %v, want %q", model2["providerID"], "anthropic")
	}
	if model2["modelID"] != "claude-opus-4-5" {
		t.Errorf("model.modelID = %v, want %q", model2["modelID"], "claude-opus-4-5")
	}

	// Check variant is at top level
	if receivedBody["variant"] != "max" {
		t.Errorf("body.variant = %v, want %q", receivedBody["variant"], "max")
	}
}

func TestSendMessageAsyncWithoutModel(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	err := client.SendMessageAsync(ctx, "ses_test123", "Hello", "/path/to/worktree", nil, "")
	if err != nil {
		t.Fatalf("SendMessageAsync error: %v", err)
	}

	// Model should be omitted
	if _, ok := receivedBody["model"]; ok {
		t.Error("model should be omitted when nil")
	}

	// Variant should be omitted when empty
	if _, ok := receivedBody["variant"]; ok {
		t.Error("variant should be omitted when empty")
	}
}

func TestNewOpenCodeClient(t *testing.T) {
	client := NewOpenCodeClient(4096)
	if client.baseURL != "http://127.0.0.1:4096" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "http://127.0.0.1:4096")
	}

	client2 := NewOpenCodeClient(5000)
	if client2.baseURL != "http://127.0.0.1:5000" {
		t.Errorf("baseURL = %q, want %q", client2.baseURL, "http://127.0.0.1:5000")
	}
}

func TestHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/global/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Healthy: true,
			Version: "1.0.0",
		})
	}))
	defer server.Close()

	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health error: %v", err)
	}

	if !health.Healthy {
		t.Error("health.Healthy should be true")
	}
	if health.Version != "1.0.0" {
		t.Errorf("health.Version = %q, want %q", health.Version, "1.0.0")
	}
}

func TestIsServerRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Healthy: true, Version: "1.0.0"})
	}))
	defer server.Close()

	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	if !client.IsServerRunning(ctx) {
		t.Error("IsServerRunning should return true for healthy server")
	}
}

func TestIsServerRunningForWorktree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/global/health" {
			json.NewEncoder(w).Encode(HealthResponse{Healthy: true, Version: "1.0.0"})
		} else if r.URL.Path == "/project/current" {
			json.NewEncoder(w).Encode(ProjectInfo{
				ID:       "proj123",
				Worktree: "/path/to/project",
			})
		}
	}))
	defer server.Close()

	client := &OpenCodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()

	// Should match
	if !client.IsServerRunningForWorktree(ctx, "/path/to/project") {
		t.Error("IsServerRunningForWorktree should return true for matching worktree")
	}

	// Should not match
	if client.IsServerRunningForWorktree(ctx, "/path/to/other") {
		t.Error("IsServerRunningForWorktree should return false for non-matching worktree")
	}
}
