//go:build semgrep_fixture

package daemon

import (
	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
)

func okRecover() {
	defer func() {
		if r := recover(); r != nil {
			logAndRepanic(nil, "okRecover", r)
		}
	}()
}

func okCheckedParserError() error {
	status, err := model.NormalizeStatus("running")
	if err != nil {
		return err
	}
	issueStatus, err := model.ParseIssueStatus("open")
	if err != nil {
		return err
	}
	apiStatus, err := orchapi.NormalizeRunStatus("running")
	if err != nil {
		return err
	}

	runProto, err := modelStatusToProto(model.StatusRunning)
	if err != nil {
		return err
	}
	runModel, err := protoStatusToModel(orchpb.RunStatus_RUN_STATUS_RUNNING)
	if err != nil {
		return err
	}
	issueProto, err := modelIssueStatusToProto(model.IssueStatusOpen)
	if err != nil {
		return err
	}
	issueModel, err := protoIssueStatusToModel(orchpb.IssueStatus_ISSUE_STATUS_OPEN)
	if err != nil {
		return err
	}

	runMessage, err := modelRunToProto(&model.Run{Status: model.StatusRunning})
	if err != nil {
		return err
	}
	runFromProto, err := protoRunToModel(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING})
	if err != nil {
		return err
	}
	issueMessage, err := modelIssueToProto(&model.Issue{Status: model.IssueStatusOpen})
	if err != nil {
		return err
	}
	issueFromProto, err := protoIssueToModel(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})
	if err != nil {
		return err
	}

	runStatuses, err := protoRunStatusSliceToModel([]orchpb.RunStatus{orchpb.RunStatus_RUN_STATUS_RUNNING})
	if err != nil {
		return err
	}
	issueStatuses, err := protoIssueStatusSliceToModel([]orchpb.IssueStatus{orchpb.IssueStatus_ISSUE_STATUS_OPEN})
	if err != nil {
		return err
	}

	clientRunProto, err := stringToProtoRunStatus("running")
	if err != nil {
		return err
	}
	clientIssueProto, err := stringToProtoIssueStatus("open")
	if err != nil {
		return err
	}
	clientRunString, err := protoRunStatusToString(orchpb.RunStatus_RUN_STATUS_RUNNING)
	if err != nil {
		return err
	}
	clientIssueString, err := protoIssueStatusToString(orchpb.IssueStatus_ISSUE_STATUS_OPEN)
	if err != nil {
		return err
	}

	runSummary, err := protoRunToSummary(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING}, nil)
	if err != nil {
		return err
	}
	runFull, err := protoRunToFull(&orchpb.Run{Status: orchpb.RunStatus_RUN_STATUS_RUNNING}, nil, nil)
	if err != nil {
		return err
	}
	issueSummary, err := protoIssueToSummary(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})
	if err != nil {
		return err
	}
	issueFull, err := protoIssueToFull(&orchpb.Issue{Status: orchpb.IssueStatus_ISSUE_STATUS_OPEN})
	if err != nil {
		return err
	}

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
	return nil
}
