package daemon

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestPopulateRunDisplayFieldsUsesProtoEnums(t *testing.T) {
	run := modelRunToProto(&model.Run{
		Status:      model.Status("custom-run-status"),
		Multiplexer: "custom-mux",
		BranchState: "custom-branch-state",
	})

	populateRunDisplayFields(run)

	if run.StatusDisplay != "" {
		t.Fatalf("StatusDisplay = %q, want empty for unknown proto status", run.StatusDisplay)
	}
	if run.MultiplexerName != "" {
		t.Fatalf("MultiplexerName = %q, want empty for unknown proto multiplexer", run.MultiplexerName)
	}
	if run.BranchStateDisplay != "" {
		t.Fatalf("BranchStateDisplay = %q, want empty for unspecified proto branch state", run.BranchStateDisplay)
	}
}

func TestModelIssueToProtoStatusDisplayUsesProtoEnum(t *testing.T) {
	issue := modelIssueToProto(&model.Issue{
		Status: model.IssueStatus("custom-issue-status"),
	})

	if issue.StatusDisplay != "" {
		t.Fatalf("StatusDisplay = %q, want empty for unknown proto issue status", issue.StatusDisplay)
	}
}
