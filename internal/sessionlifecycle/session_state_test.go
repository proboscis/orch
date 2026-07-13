package sessionlifecycle

import (
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/model"
)

func TestDeriveUsesClosedStoredFactVocabulary(t *testing.T) {
	reapedNote := model.NewDaemonNoticeEvent("session_reaped", map[string]string{"generation": "1"})
	cases := []struct {
		name       string
		run        *model.Run
		want       model.SessionState
		wantDetail string
	}{
		{
			name: "live",
			run:  &model.Run{Agent: "codex"},
			want: model.SessionStateLive,
		},
		{
			name: "reaped revivable",
			run: &model.Run{
				IssueID:                "issue-1",
				RunID:                  "20260713-101112",
				Agent:                  "codex",
				AgentSessionID:         "rollout-1",
				AgentSessionGeneration: 1,
				WorktreePath:           "/tmp/worktree",
				Multiplexer:            "tmux",
				Events:                 []*model.Event{reapedNote},
			},
			want: model.SessionStateReapedRevivable,
		},
		{
			name: "reaped unrevivable",
			run: &model.Run{
				IssueID:      "issue-1",
				RunID:        "20260713-101113",
				Agent:        "codex",
				WorktreePath: "/tmp/worktree",
				Multiplexer:  "tmux",
				Events:       []*model.Event{reapedNote},
			},
			want:       model.SessionStateReapedUnrevivable,
			wantDetail: "agent_session identity is not recorded",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, detail := Derive(tc.run)
			if got != tc.want {
				t.Fatalf("Derive() state = %q, want %q", got, tc.want)
			}
			if !strings.Contains(detail, tc.wantDetail) {
				t.Fatalf("Derive() detail = %q, want substring %q", detail, tc.wantDetail)
			}
		})
	}
}

func TestDerivePreservesVersionedEventlessIndexValue(t *testing.T) {
	run := &model.Run{
		SessionState:       model.SessionStateReapedUnrevivable,
		SessionStateDetail: "cached missing precondition",
	}
	state, detail := Derive(run)
	if state != model.SessionStateReapedUnrevivable || detail != "cached missing precondition" {
		t.Fatalf("Derive(eventless cache) = %q detail=%q", state, detail)
	}
	if !run.SessionReaped() {
		t.Fatal("eventless cached reaped state must preserve the SessionReaped latch")
	}
}
