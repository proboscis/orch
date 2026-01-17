// Package multiplexer provides an abstraction layer for terminal multiplexers (tmux, zellij).
package multiplexer

import (
	"fmt"
	"os"
	"time"
)

// Type represents the type of terminal multiplexer.
type Type string

const (
	TypeTmux   Type = "tmux"
	TypeZellij Type = "zellij"
)

// ParseType parses a string into a Type.
func ParseType(s string) (Type, error) {
	switch s {
	case "tmux", "":
		return TypeTmux, nil
	case "zellij":
		return TypeZellij, nil
	default:
		return "", fmt.Errorf("unknown multiplexer type: %s", s)
	}
}

// String returns the string representation of the Type.
func (t Type) String() string {
	return string(t)
}

// SessionConfig holds configuration for creating a multiplexer session.
type SessionConfig struct {
	SessionName string
	WorkDir     string
	Command     string   // Command to run in the session
	Env         []string // Environment variables (KEY=VALUE format)
	WindowName  string
}

// Window describes a multiplexer window.
type Window struct {
	Index int
	Name  string
	ID    string
}

// Pane describes a multiplexer pane.
type Pane struct {
	ID      string
	Index   int
	Title   string
	Command string
}

// Multiplexer defines the interface for terminal multiplexer operations.
type Multiplexer interface {
	// Type returns the type of this multiplexer.
	Type() Type

	// IsAvailable checks if the multiplexer is installed and accessible.
	IsAvailable() bool

	// IsInsideSession returns true if we're currently inside this multiplexer.
	IsInsideSession() bool

	// Session operations
	HasSession(name string) bool
	NewSession(cfg *SessionConfig) error
	AttachSession(session string) error
	KillSession(session string) error
	ListSessions() ([]string, error)

	// Window operations
	ListWindows(session string) ([]Window, error)
	NewWindow(session, name, workDir, command string) error
	SelectWindow(session string, index int) error
	SelectWindowByID(windowID string) error
	RenameWindow(session string, index int, name string) error

	// Pane operations
	ListPanes(target string) ([]Pane, error)
	SplitWindow(target string, vertical bool, percent int) (string, error)
	SelectPane(target string) error
	SetPaneTitle(target, title string) error
	KillPane(target string) error
	SwapPane(source, target string) error

	// I/O operations
	SendKeys(session, keys string) error
	SendKeysLiteral(session, keys string) error
	SendText(session, text string) error
	CapturePane(session string, lines int) (string, error)
	WaitForReady(session, pattern string, timeout time.Duration) error

	// Session management
	SwitchClient(session string) error
	CurrentSession() (string, error)

	// Options
	SetOption(session, option, value string) error
	GetOption(session, option string) (string, error)

	// Window linking (for monitor functionality)
	LinkWindow(sourceSession string, sourceWindow int, targetSession string, targetIndex int) error
	LinkWindowByID(windowID, targetSession string, targetIndex int) error
	UnlinkWindow(session string, index int) error

	// Pane commands for batch operations
	ListPaneCommands() (map[string][]string, error)
	AgentAlive(session string, paneCommands map[string][]string) (bool, bool)
}

// GetMultiplexer returns a Multiplexer implementation for the given type.
func GetMultiplexer(t Type) (Multiplexer, error) {
	switch t {
	case TypeTmux:
		return NewTmuxMultiplexer(), nil
	case TypeZellij:
		return NewZellijMultiplexer(), nil
	default:
		return nil, fmt.Errorf("unknown multiplexer type: %s", t)
	}
}

// GetDefault returns the default multiplexer (tmux) or the one configured via ORCH_MULTIPLEXER.
func GetDefault() Multiplexer {
	if muxType := os.Getenv("ORCH_MULTIPLEXER"); muxType != "" {
		t, err := ParseType(muxType)
		if err == nil {
			mux, err := GetMultiplexer(t)
			if err == nil && mux.IsAvailable() {
				return mux
			}
		}
	}
	// Default to tmux
	return NewTmuxMultiplexer()
}

// GetWithFallback returns a multiplexer of the given type, or falls back to an available one.
func GetWithFallback(preferred Type) (Multiplexer, error) {
	mux, err := GetMultiplexer(preferred)
	if err != nil {
		return nil, err
	}

	if mux.IsAvailable() {
		return mux, nil
	}

	// Try fallback
	alternatives := []Type{TypeTmux, TypeZellij}
	for _, alt := range alternatives {
		if alt == preferred {
			continue
		}
		altMux, err := GetMultiplexer(alt)
		if err == nil && altMux.IsAvailable() {
			return altMux, fmt.Errorf("%s not available, falling back to %s", preferred, alt)
		}
	}

	return nil, fmt.Errorf("%s is not available and no fallback multiplexer found", preferred)
}
