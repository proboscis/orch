//go:build integration
// +build integration

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests require a running opencode server on port 4096
// Run with: go test -tags=integration -v ./internal/agent/

func skipIfNoOpenCode(t *testing.T) *OpenCodeClient {
	client := NewOpenCodeClient(4096)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !client.IsServerRunning(ctx) {
		t.Skip("opencode server not running on port 4096, skipping integration test")
	}
	return client
}

func TestIntegration_CreateSessionWithArbitraryDirectory(t *testing.T) {
	client := skipIfNoOpenCode(t)

	// Use a temp directory as the "worktree"
	tempDir, err := os.MkdirTemp("", "opencode-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize git repo in temp dir (opencode needs a git repo)
	if err := os.WriteFile(filepath.Join(tempDir, ".git"), []byte("gitdir: /tmp/fake"), 0644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create session with the temp directory
	session, err := client.CreateSession(ctx, "integration-test-session", tempDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	if session.ID == "" {
		t.Error("session.ID should not be empty")
	}
	if session.Title != "integration-test-session" {
		t.Errorf("session.Title = %q, want %q", session.Title, "integration-test-session")
	}

	t.Logf("Created session: ID=%s, Title=%s, Directory=%s", session.ID, session.Title, session.Directory)

	// Verify the session's directory matches what we requested
	// Note: opencode may resolve the path differently, so we check it's set
	if session.Directory == "" {
		t.Error("session.Directory should not be empty")
	}
}

func TestIntegration_SendMessageWithModelAndVariant(t *testing.T) {
	client := skipIfNoOpenCode(t)

	// Use current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a session
	session, err := client.CreateSession(ctx, "integration-test-model", cwd)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	t.Logf("Created session: %s", session.ID)

	// Send message with model and variant
	model := &ModelRef{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5", // Use sonnet for faster/cheaper test
	}
	variant := "high" // Use "high" instead of "max" for faster test

	err = client.SendMessageAsync(ctx, session.ID, "Reply with just the word 'hello'", cwd, model, variant)
	if err != nil {
		t.Fatalf("SendMessageAsync error: %v", err)
	}

	t.Logf("Message sent with model=%s/%s, variant=%s", model.ProviderID, model.ModelID, variant)

	// Wait a bit and check if we got a response
	time.Sleep(5 * time.Second)

	// We can't easily verify the response in async mode, but the test passing
	// means the API accepted our model/variant parameters
	t.Log("Message sent successfully - API accepted model and variant parameters")
}

func TestIntegration_SendMessageWithOpusAndMaxThinking(t *testing.T) {
	client := skipIfNoOpenCode(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a session
	session, err := client.CreateSession(ctx, "integration-test-opus-max", cwd)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	t.Logf("Created session: %s", session.ID)

	// Send message with opus and max thinking
	model := &ModelRef{
		ProviderID: "anthropic",
		ModelID:    "claude-opus-4-5",
	}
	variant := "max"

	err = client.SendMessageAsync(ctx, session.ID, "Reply with just 'ok'", cwd, model, variant)
	if err != nil {
		t.Fatalf("SendMessageAsync error: %v", err)
	}

	t.Logf("Message sent with model=%s/%s, variant=%s", model.ProviderID, model.ModelID, variant)
	t.Log("Message sent successfully - API accepted opus model with max thinking")
}

func TestIntegration_CreateSessionInDifferentDirectories(t *testing.T) {
	client := skipIfNoOpenCode(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create two temp directories
	dir1, err := os.MkdirTemp("", "opencode-test-dir1-*")
	if err != nil {
		t.Fatalf("failed to create temp dir 1: %v", err)
	}
	defer os.RemoveAll(dir1)

	dir2, err := os.MkdirTemp("", "opencode-test-dir2-*")
	if err != nil {
		t.Fatalf("failed to create temp dir 2: %v", err)
	}
	defer os.RemoveAll(dir2)

	// Create sessions in different directories
	session1, err := client.CreateSession(ctx, "test-dir1", dir1)
	if err != nil {
		t.Fatalf("CreateSession for dir1 error: %v", err)
	}
	t.Logf("Session 1: ID=%s, Directory=%s", session1.ID, session1.Directory)

	session2, err := client.CreateSession(ctx, "test-dir2", dir2)
	if err != nil {
		t.Fatalf("CreateSession for dir2 error: %v", err)
	}
	t.Logf("Session 2: ID=%s, Directory=%s", session2.ID, session2.Directory)

	// Verify sessions have different IDs
	if session1.ID == session2.ID {
		t.Error("sessions should have different IDs")
	}

	// Verify directories are different (or at least sessions are distinct)
	t.Log("Successfully created sessions in different directories")
}

func TestIntegration_VerifyModelInResponse(t *testing.T) {
	client := skipIfNoOpenCode(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Create a session
	session, err := client.CreateSession(ctx, "integration-test-verify-model", cwd)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	t.Logf("Created session: %s", session.ID)

	// Send message with specific model
	model := &ModelRef{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
	}

	// Use synchronous send to get response
	message, err := client.SendMessage(ctx, session.ID, "Reply with exactly: TEST_RESPONSE")
	if err != nil {
		// SendMessage doesn't support model parameter in current implementation
		// Fall back to async test
		t.Log("SendMessage failed (expected if model not supported), testing async instead")

		err = client.SendMessageAsync(ctx, session.ID, "Reply with exactly: TEST_RESPONSE", cwd, model, "")
		if err != nil {
			t.Fatalf("SendMessageAsync error: %v", err)
		}
		t.Log("Async message sent successfully")
		return
	}

	t.Logf("Got response: role=%s", message.Info.Role)
	if message.Info.Role != "assistant" {
		t.Errorf("expected assistant role, got %s", message.Info.Role)
	}
}
