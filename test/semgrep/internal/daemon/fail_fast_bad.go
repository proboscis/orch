//go:build semgrep_fixture

package daemon

import (
	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
)

func badRecoverIncompleteIf() {
	defer func() {
		if r := recover(); r != nil {
			println(r)
		}
	}()
}

func badRecoverBare() {
	defer func() {
		recover()
	}()
}

func badRecoverDiscarded() {
	defer func() {
		_ = recover()
	}()
}

func badRecoverAssignedButUnchecked() {
	defer func() {
		r := recover()
		_ = r
	}()
}

func badDiscardedParserErrors() {
	status, _ := model.NormalizeStatus("running")
	issueStatus, _ := model.ParseIssueStatus("open")
	apiStatus, _ := orchapi.NormalizeRunStatus("running")

	runProto, _ := modelStatusToProto(model.StatusRunning)
	runModel, _ := protoStatusToModel(orchpb.RunStatus_RUN_STATUS_RUNNING)
	issueProto, _ := modelIssueStatusToProto(model.IssueStatusOpen)
	issueModel, _ := protoIssueStatusToModel(orchpb.IssueStatus_ISSUE_STATUS_OPEN)

	runMessage, _ := modelRunToProto(&model.Run{Status: model.StatusRunning})
	runFromProto, _ := protoRunToModel(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING})
	issueMessage, _ := modelIssueToProto(&model.Issue{Status: model.IssueStatusOpen})
	issueFromProto, _ := protoIssueToModel(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})

	runStatuses, _ := protoRunStatusSliceToModel([]orchpb.RunStatus{orchpb.RunStatus_RUN_STATUS_RUNNING})
	issueStatuses, _ := protoIssueStatusSliceToModel([]orchpb.IssueStatus{orchpb.IssueStatus_ISSUE_STATUS_OPEN})

	clientRunProto, _ := stringToProtoRunStatus("running")
	clientIssueProto, _ := stringToProtoIssueStatus("open")
	clientRunString, _ := protoRunStatusToString(orchpb.RunStatus_RUN_STATUS_RUNNING)
	clientIssueString, _ := protoIssueStatusToString(orchpb.IssueStatus_ISSUE_STATUS_OPEN)

	runSummary, _ := protoRunToSummary(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING}, nil)
	runFull, _ := protoRunToFull(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING}, nil, nil)
	issueSummary, _ := protoIssueToSummary(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})
	issueFull, _ := protoIssueToFull(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})

	_, _ = model.ParseIssueStatus("open")
	_, _ = protoRunToFull(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING}, nil, nil)

	_ = status
	_ = issueStatus
	_ = apiStatus
	_ = runProto
	_ = runModel
	_ = issueProto
	_ = issueModel
	_ = runMessage
	_ = runFromProto
	_ = issueMessage
	_ = issueFromProto
	_ = runStatuses
	_ = issueStatuses
	_ = clientRunProto
	_ = clientIssueProto
	_ = clientRunString
	_ = clientIssueString
	_ = runSummary
	_ = runFull
	_ = issueSummary
	_ = issueFull
}
