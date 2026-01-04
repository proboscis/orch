package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Logger interface {
	Printf(format string, args ...interface{})
}

type OpenCodeClient struct {
	baseURL    string
	port       int
	httpClient *http.Client
	debug      Logger
}

func NewOpenCodeClient(port int) *OpenCodeClient {
	return &OpenCodeClient{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		port:    port,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *OpenCodeClient) SetDebugLogger(logger Logger) {
	c.debug = logger
}

func (c *OpenCodeClient) logDebug(format string, args ...interface{}) {
	if c.debug != nil {
		c.debug.Printf(format, args...)
	}
}

func (c *OpenCodeClient) Port() int {
	return c.port
}

// retry executes fn with exponential backoff
// Returns the result of fn or the last error after maxRetries attempts
func retry[T any](ctx context.Context, maxRetries int, initialDelay time.Duration, fn func() (T, error)) (T, error) {
	var result T
	var err error
	delay := initialDelay

	for i := 0; i < maxRetries; i++ {
		result, err = fn()
		if err == nil {
			return result, nil
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Wait before retry with exponential backoff
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(delay):
				delay = delay * 2
				if delay > 10*time.Second {
					delay = 10 * time.Second
				}
			}
		}
	}

	return result, err
}

// retryNoResult executes fn with exponential backoff for functions that don't return a value
func retryNoResult(ctx context.Context, maxRetries int, initialDelay time.Duration, fn func() error) error {
	_, err := retry(ctx, maxRetries, initialDelay, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// HealthResponse represents the response from /global/health
type HealthResponse struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// Session represents an opencode session
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Directory string    `json:"directory,omitempty"`
	ParentID  string    `json:"parentID,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MessagePart represents a part of a message
type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Message represents an opencode message
type Message struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts"`
}

// MessageInfo contains message metadata
type MessageInfo struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// PromptRequest represents a request to send a prompt
type PromptRequest struct {
	Parts   []MessagePart `json:"parts"`
	Model   *ModelRef     `json:"model,omitempty"`
	Variant string        `json:"variant,omitempty"` // Thinking mode: "high", "max", etc.
}

// ModelRef specifies the model to use
type ModelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// ParseModel parses a model string in "provider/model" format
func ParseModel(model string) *ModelRef {
	if model == "" {
		return nil
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	return &ModelRef{
		ProviderID: parts[0],
		ModelID:    parts[1],
	}
}

// Event represents an SSE event from opencode
type Event struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

func (c *OpenCodeClient) IsServerRunning(ctx context.Context) bool {
	c.logDebug("Checking if server is running on port %d...", c.port)
	health, err := c.Health(ctx)
	running := err == nil && health.Healthy
	if running {
		c.logDebug("Server is running on port %d", c.port)
	} else {
		c.logDebug("No server running on port %d", c.port)
	}
	return running
}

// ProjectInfo represents the current project info from opencode
type ProjectInfo struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
}

// GetCurrentProject returns the current project info from the server
func (c *OpenCodeClient) GetCurrentProject(ctx context.Context) (*ProjectInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/project/current", nil)
	if err != nil {
		return nil, fmt.Errorf("creating project request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get project returned status %d", resp.StatusCode)
	}

	var project ProjectInfo
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("decoding project response: %w", err)
	}

	return &project, nil
}

// IsServerRunningForWorktree checks if the server is running AND serving the specified worktree
func (c *OpenCodeClient) IsServerRunningForWorktree(ctx context.Context, worktreePath string) bool {
	if !c.IsServerRunning(ctx) {
		return false
	}

	project, err := c.GetCurrentProject(ctx)
	if err != nil {
		return false
	}

	// Check if the server's worktree matches the expected worktree
	return project.Worktree == worktreePath
}

func (c *OpenCodeClient) Health(ctx context.Context) (*HealthResponse, error) {
	url := c.baseURL + "/global/health"
	c.logDebug("Health check: GET %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logDebug("Health check failed: %v", err)
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logDebug("Health check returned status %d", resp.StatusCode)
		return nil, fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decoding health response: %w", err)
	}

	c.logDebug("Health check: %d OK (healthy=%v, version=%s)", resp.StatusCode, health.Healthy, health.Version)
	return &health, nil
}

// WaitForHealthy waits until the server is healthy or context is cancelled
func (c *OpenCodeClient) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for opencode server to be healthy: %w", ctx.Err())
		case <-ticker.C:
			health, err := c.Health(ctx)
			if err == nil && health.Healthy {
				return nil
			}
		}
	}
}

// CreateSession creates a new session with retry logic
// The directory parameter specifies the working directory for the session
func (c *OpenCodeClient) CreateSession(ctx context.Context, title, directory string) (*Session, error) {
	return retry(ctx, 5, 500*time.Millisecond, func() (*Session, error) {
		return c.createSessionOnce(ctx, title, directory)
	})
}

func (c *OpenCodeClient) createSessionOnce(ctx context.Context, title, directory string) (*Session, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling session request: %w", err)
	}

	url := c.baseURL + "/session"
	c.logDebug("Creating session: POST %s", url)
	c.logDebug("  X-OpenCode-Directory: %s", directory)
	c.logDebug("  Body: %s", string(jsonBody))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logDebug("Create session failed: %v", err)
		return nil, fmt.Errorf("creating session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		c.logDebug("Create session returned status %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("create session returned status %d: %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decoding session response: %w", err)
	}

	c.logDebug("Session created: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	c.logDebug("  Session ID: %s", session.ID)
	c.logDebug("  Directory: %s", session.Directory)
	return &session, nil
}

// SendMessage sends a message to a session and waits for the response
func (c *OpenCodeClient) SendMessage(ctx context.Context, sessionID, text string) (*Message, error) {
	reqBody := PromptRequest{
		Parts: []MessagePart{
			{Type: "text", Text: text},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling message request: %w", err)
	}

	// Use a client without timeout for potentially long-running requests
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session/"+sessionID+"/message", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating message request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("send message returned status %d: %s", resp.StatusCode, string(body))
	}

	var message Message
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		return nil, fmt.Errorf("decoding message response: %w", err)
	}

	return &message, nil
}

// SendMessageAsync sends a message asynchronously with retry logic (does not wait for response)
// The directory parameter specifies the working directory context for the request
// The variant parameter specifies thinking mode: "high", "max", etc.
func (c *OpenCodeClient) SendMessageAsync(ctx context.Context, sessionID, text, directory string, model *ModelRef, variant string) error {
	return retryNoResult(ctx, 3, 500*time.Millisecond, func() error {
		return c.sendMessageAsyncOnce(ctx, sessionID, text, directory, model, variant)
	})
}

// SendMessagePrompt sends a message via /message endpoint (triggers agent execution, unlike prompt_async).
// Queues message if agent is busy. TUI clients won't auto-refresh; they need reload to see the message.
func (c *OpenCodeClient) SendMessagePrompt(ctx context.Context, sessionID, text, directory string) error {
	return retryNoResult(ctx, 3, 500*time.Millisecond, func() error {
		return c.sendMessagePromptOnce(ctx, sessionID, text, directory)
	})
}

func (c *OpenCodeClient) sendMessagePromptOnce(ctx context.Context, sessionID, text, directory string) error {
	reqBody := PromptRequest{
		Parts: []MessagePart{
			{Type: "text", Text: text},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling prompt request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session/"+sessionID+"/message", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating prompt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending prompt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send prompt returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *OpenCodeClient) sendMessageAsyncOnce(ctx context.Context, sessionID, text, directory string, model *ModelRef, variant string) error {
	reqBody := PromptRequest{
		Parts: []MessagePart{
			{Type: "text", Text: text},
		},
		Model:   model,
		Variant: variant,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling message request: %w", err)
	}

	url := c.baseURL + "/session/" + sessionID + "/prompt_async"
	c.logDebug("Sending async prompt: POST %s", url)
	c.logDebug("  X-OpenCode-Directory: %s", directory)
	if model != nil {
		c.logDebug("  Model: %s/%s", model.ProviderID, model.ModelID)
	}
	if variant != "" {
		c.logDebug("  Variant: %s", variant)
	}
	c.logDebug("  Prompt length: %d bytes", len(text))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating async message request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logDebug("Send async prompt failed: %v", err)
		return fmt.Errorf("sending async message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logDebug("Send async prompt returned status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("send async message returned status %d: %s", resp.StatusCode, string(body))
	}

	c.logDebug("Prompt sent: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	return nil
}

// Abort aborts a running session
func (c *OpenCodeClient) Abort(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session/"+sessionID+"/abort", nil)
	if err != nil {
		return fmt.Errorf("creating abort request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("aborting session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("abort returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SubscribeEvents subscribes to SSE events from the server
// Returns a channel that receives events. The channel is closed when the context is cancelled.
func (c *OpenCodeClient) SubscribeEvents(ctx context.Context) (<-chan Event, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/event", nil)
	if err != nil {
		return nil, fmt.Errorf("creating events request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a client without timeout for SSE
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subscribing to events: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("events subscription returned status %d", resp.StatusCode)
	}

	events := make(chan Event, 100)

	go func() {
		defer resp.Body.Close()
		defer close(events)

		reader := bufio.NewReader(resp.Body)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}

				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				// Parse SSE format: "data: {...}"
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					var event Event
					if err := json.Unmarshal([]byte(data), &event); err != nil {
						continue // Skip malformed events
					}

					select {
					case events <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return events, nil
}

// GetSessions lists all sessions
func (c *OpenCodeClient) GetSessions(ctx context.Context) ([]Session, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session", nil)
	if err != nil {
		return nil, fmt.Errorf("creating sessions request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions returned status %d", resp.StatusCode)
	}

	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decoding sessions response: %w", err)
	}

	return sessions, nil
}

// GetSessionsForDirectory lists sessions for a specific directory
func (c *OpenCodeClient) GetSessionsForDirectory(ctx context.Context, directory string) ([]Session, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session", nil)
	if err != nil {
		return nil, fmt.Errorf("creating sessions request: %w", err)
	}
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions returned status %d", resp.StatusCode)
	}

	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decoding sessions response: %w", err)
	}

	return sessions, nil
}

// GetSession gets a specific session by ID
func (c *OpenCodeClient) GetSession(ctx context.Context, sessionID, directory string) (*Session, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating session request: %w", err)
	}
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get session returned status %d: %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decoding session response: %w", err)
	}

	return &session, nil
}

type ProviderInfo struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Models []ModelInfo `json:"models"`
}

type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Variants    []string `json:"variants,omitempty"`
	Attachments bool     `json:"attachments"`
}

type ProvidersResponse struct {
	All      []ProviderInfo `json:"all"`
	Thinking []ProviderInfo `json:"thinking"`
}

func (c *OpenCodeClient) GetSessionIDs(ctx context.Context) (map[string]bool, error) {
	sessions, err := c.GetSessions(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		ids[s.ID] = true
	}
	return ids, nil
}

func (c *OpenCodeClient) GetProviders(ctx context.Context) (*ProvidersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/provider", nil)
	if err != nil {
		return nil, fmt.Errorf("creating providers request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing providers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list providers returned status %d: %s", resp.StatusCode, string(body))
	}

	var providers ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("decoding providers response: %w", err)
	}

	return &providers, nil
}

type SessionStatus string

const (
	SessionStatusIdle  SessionStatus = "idle"
	SessionStatusBusy  SessionStatus = "busy"
	SessionStatusRetry SessionStatus = "retry"
)

func (c *OpenCodeClient) GetSessionStatus(ctx context.Context, directory string) (map[string]SessionStatus, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/status", nil)
	if err != nil {
		return nil, fmt.Errorf("creating session status request: %w", err)
	}
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting session status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get session status returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading session status response: %w", err)
	}

	return parseSessionStatusResponse(body)
}

func parseSessionStatusResponse(body []byte) (map[string]SessionStatus, error) {
	var stringMap map[string]string
	if err := json.Unmarshal(body, &stringMap); err == nil {
		result := make(map[string]SessionStatus, len(stringMap))
		for k, v := range stringMap {
			result[k] = SessionStatus(v)
		}
		return result, nil
	}

	var objectMap map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &objectMap); err == nil {
		result := make(map[string]SessionStatus, len(objectMap))
		for k, v := range objectMap {
			result[k] = SessionStatus(v.Type)
		}
		return result, nil
	}

	return nil, fmt.Errorf("unable to parse session status response: %s", string(body))
}

func (c *OpenCodeClient) GetSingleSessionStatus(ctx context.Context, sessionID, directory string) (SessionStatus, bool, error) {
	statusMap, err := c.GetSessionStatus(ctx, directory)
	if err != nil {
		return "", false, err
	}

	status, ok := statusMap[sessionID]
	if !ok {
		return SessionStatusIdle, false, nil
	}

	return status, true, nil
}

func (c *OpenCodeClient) GetMessages(ctx context.Context, sessionID, directory string) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sessionID+"/message", nil)
	if err != nil {
		return nil, fmt.Errorf("creating messages request: %w", err)
	}
	if directory != "" {
		req.Header.Set("X-OpenCode-Directory", directory)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get messages returned status %d: %s", resp.StatusCode, string(body))
	}

	var messages []Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decoding messages response: %w", err)
	}

	return messages, nil
}

type AgentConfig struct {
	Model string `json:"model"`
}

type OpenCodeConfig struct {
	Agent map[string]AgentConfig `json:"agent"`
}

func (c *OpenCodeClient) GetAgentModel(ctx context.Context) (model string, variant string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/config", nil)
	if err != nil {
		return "", "", fmt.Errorf("creating config request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("getting config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("get config returned status %d", resp.StatusCode)
	}

	var config OpenCodeConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return "", "", fmt.Errorf("decoding config response: %w", err)
	}

	for _, agentCfg := range config.Agent {
		if agentCfg.Model != "" {
			return agentCfg.Model, "", nil
		}
	}

	return "", "", nil
}
