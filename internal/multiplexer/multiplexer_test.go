package multiplexer

import (
	"errors"
	"testing"
)

func TestParseType(t *testing.T) {
	tests := []struct {
		input   string
		want    Type
		wantErr bool
	}{
		{"tmux", TypeTmux, false},
		{"", TypeTmux, false}, // empty defaults to tmux
		{"zellij", TypeZellij, false},
		{"invalid", "", true},
		{"TMUX", "", true}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTypeString(t *testing.T) {
	tests := []struct {
		t    Type
		want string
	}{
		{TypeTmux, "tmux"},
		{TypeZellij, "zellij"},
	}

	for _, tt := range tests {
		t.Run(string(tt.t), func(t *testing.T) {
			if got := tt.t.String(); got != tt.want {
				t.Fatalf("Type.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMultiplexer(t *testing.T) {
	tests := []struct {
		t       Type
		wantTyp Type
		wantErr bool
	}{
		{TypeTmux, TypeTmux, false},
		{TypeZellij, TypeZellij, false},
		{Type("invalid"), "", true},
	}

	for _, tt := range tests {
		t.Run(string(tt.t), func(t *testing.T) {
			mux, err := GetMultiplexer(tt.t)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetMultiplexer(%v) error = %v, wantErr %v", tt.t, err, tt.wantErr)
			}
			if err == nil && mux.Type() != tt.wantTyp {
				t.Fatalf("GetMultiplexer(%v).Type() = %v, want %v", tt.t, mux.Type(), tt.wantTyp)
			}
		})
	}
}

func TestGetDefault(t *testing.T) {
	// Without env, should default to tmux
	mux := GetDefault()
	if mux.Type() != TypeTmux {
		t.Fatalf("GetDefault() = %v, want %v", mux.Type(), TypeTmux)
	}
}

func TestSessionConfig(t *testing.T) {
	cfg := &SessionConfig{
		SessionName: "test-session",
		WorkDir:     "/test/dir",
		Command:     "echo hello",
		Env:         []string{"FOO=bar"},
		WindowName:  "main",
	}

	if cfg.SessionName != "test-session" {
		t.Fatal("SessionName mismatch")
	}
	if cfg.WorkDir != "/test/dir" {
		t.Fatal("WorkDir mismatch")
	}
	if cfg.Command != "echo hello" {
		t.Fatal("Command mismatch")
	}
	if len(cfg.Env) != 1 || cfg.Env[0] != "FOO=bar" {
		t.Fatal("Env mismatch")
	}
	if cfg.WindowName != "main" {
		t.Fatal("WindowName mismatch")
	}
}

func TestWindowStruct(t *testing.T) {
	w := Window{
		Index: 1,
		Name:  "test-window",
		ID:    "@1",
	}

	if w.Index != 1 {
		t.Fatal("Index mismatch")
	}
	if w.Name != "test-window" {
		t.Fatal("Name mismatch")
	}
	if w.ID != "@1" {
		t.Fatal("ID mismatch")
	}
}

func TestPaneStruct(t *testing.T) {
	p := Pane{
		ID:      "%1",
		Index:   0,
		Title:   "test-pane",
		Command: "bash",
	}

	if p.ID != "%1" {
		t.Fatal("ID mismatch")
	}
	if p.Index != 0 {
		t.Fatal("Index mismatch")
	}
	if p.Title != "test-pane" {
		t.Fatal("Title mismatch")
	}
	if p.Command != "bash" {
		t.Fatal("Command mismatch")
	}
}

func TestErrUnsupported(t *testing.T) {
	err := unsupportedErr("test feature")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected errors.Is(err, ErrUnsupported) to be true")
	}
	if err.Error() != "multiplexer: operation not supported: test feature" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
