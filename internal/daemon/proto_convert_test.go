package daemon

import (
	"testing"

	"github.com/proboscis/orch/api/orchpb"
)

func TestPopulateRunDisplayFieldsUsesProtoEnums(t *testing.T) {
	run := &orchpb.Run{
		Status:      orchpb.RunStatus_RUN_STATUS_UNKNOWN,
		Multiplexer: orchpb.Multiplexer_MULTIPLEXER_UNSPECIFIED,
		BranchState: orchpb.BranchState_BRANCH_STATE_UNSPECIFIED,
	}

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

func TestPopulateIssueDisplayFieldsUsesProtoEnum(t *testing.T) {
	issue := &orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_UNSPECIFIED}
	populateIssueDisplayFields(issue)

	if issue.StatusDisplay != "" {
		t.Fatalf("StatusDisplay = %q, want empty for unspecified proto issue status", issue.StatusDisplay)
	}
}
