package daemon

import (
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
)

func modelStatusToProto(s model.Status) orchpb.RunStatus {
	switch s {
	case model.StatusQueued:
		return orchpb.RunStatus_RUN_STATUS_QUEUED
	case model.StatusBooting:
		return orchpb.RunStatus_RUN_STATUS_BOOTING
	case model.StatusRunning:
		return orchpb.RunStatus_RUN_STATUS_RUNNING
	case model.StatusWaiting:
		return orchpb.RunStatus_RUN_STATUS_WAITING
	case model.StatusRateLimited:
		return orchpb.RunStatus_RUN_STATUS_RATE_LIMITED
	case model.StatusPROpen:
		return orchpb.RunStatus_RUN_STATUS_PR_OPEN
	case model.StatusDone:
		return orchpb.RunStatus_RUN_STATUS_DONE
	case model.StatusFailed:
		return orchpb.RunStatus_RUN_STATUS_FAILED
	case model.StatusCanceled:
		return orchpb.RunStatus_RUN_STATUS_CANCELED
	default:
		return orchpb.RunStatus_RUN_STATUS_UNSPECIFIED
	}
}

func protoStatusToModel(s orchpb.RunStatus) model.Status {
	switch s {
	case orchpb.RunStatus_RUN_STATUS_QUEUED:
		return model.StatusQueued
	case orchpb.RunStatus_RUN_STATUS_BOOTING:
		return model.StatusBooting
	case orchpb.RunStatus_RUN_STATUS_RUNNING:
		return model.StatusRunning
	case orchpb.RunStatus_RUN_STATUS_WAITING:
		return model.StatusWaiting
	case orchpb.RunStatus_RUN_STATUS_RATE_LIMITED:
		return model.StatusRateLimited
	case orchpb.RunStatus_RUN_STATUS_PR_OPEN:
		return model.StatusPROpen
	case orchpb.RunStatus_RUN_STATUS_DONE:
		return model.StatusDone
	case orchpb.RunStatus_RUN_STATUS_FAILED:
		return model.StatusFailed
	case orchpb.RunStatus_RUN_STATUS_CANCELED:
		return model.StatusCanceled
	default:
		return model.StatusQueued
	}
}

func modelIssueStatusToProto(s model.IssueStatus) orchpb.IssueStatus {
	switch s {
	case model.IssueStatusOpen:
		return orchpb.IssueStatus_ISSUE_STATUS_OPEN
	case model.IssueStatusResolved:
		return orchpb.IssueStatus_ISSUE_STATUS_RESOLVED
	case model.IssueStatusClosed:
		return orchpb.IssueStatus_ISSUE_STATUS_CLOSED
	default:
		return orchpb.IssueStatus_ISSUE_STATUS_UNSPECIFIED
	}
}

func protoIssueStatusToModel(s orchpb.IssueStatus) model.IssueStatus {
	switch s {
	case orchpb.IssueStatus_ISSUE_STATUS_OPEN:
		return model.IssueStatusOpen
	case orchpb.IssueStatus_ISSUE_STATUS_RESOLVED:
		return model.IssueStatusResolved
	case orchpb.IssueStatus_ISSUE_STATUS_CLOSED:
		return model.IssueStatusClosed
	default:
		return model.IssueStatusOpen
	}
}

func multiplexerToProto(mux string) orchpb.Multiplexer {
	switch mux {
	case "tmux":
		return orchpb.Multiplexer_MULTIPLEXER_TMUX
	case "zellij":
		return orchpb.Multiplexer_MULTIPLEXER_ZELLIJ
	default:
		return orchpb.Multiplexer_MULTIPLEXER_UNSPECIFIED
	}
}

func protoToMultiplexer(mux orchpb.Multiplexer) string {
	switch mux {
	case orchpb.Multiplexer_MULTIPLEXER_TMUX:
		return "tmux"
	case orchpb.Multiplexer_MULTIPLEXER_ZELLIJ:
		return "zellij"
	default:
		return ""
	}
}

func modelRunToProto(run *model.Run) *orchpb.Run {
	if run == nil {
		return nil
	}

	protoRun := &orchpb.Run{
		IssueId:           sanitizeUTF8(run.IssueID),
		RunId:             sanitizeUTF8(run.RunID),
		Status:            modelStatusToProto(run.Status),
		Agent:             sanitizeUTF8(run.Agent),
		Model:             sanitizeUTF8(run.Model),
		Branch:            sanitizeUTF8(run.Branch),
		WorktreePath:      sanitizeUTF8(run.WorktreePath),
		PrUrl:             sanitizeUTF8(run.PRUrl),
		StartedAtUnix:     run.StartedAt.Unix(),
		UpdatedAtUnix:     run.UpdatedAt.Unix(),
		ElapsedSeconds:    int32(run.UpdatedAt.Sub(run.StartedAt).Seconds()),
		SessionName:       sanitizeUTF8(run.SessionName),
		Multiplexer:       multiplexerToProto(run.Multiplexer),
		ServerPort:        int32(run.ServerPort),
		OpencodeSessionId: sanitizeUTF8(run.OpenCodeSessionID),
		ContinuedFrom:     sanitizeUTF8(run.ContinuedFrom),
		PrNumber:          int32(run.PRNumber),
		PrState:           sanitizeUTF8(run.PRState),
		Alive:             run.Alive,
		AliveKnown:        run.AliveKnown,
		WorktreeExists:    run.WorktreeExists,
	}

	return protoRun
}

func protoRunToModel(run *orchpb.Run) *model.Run {
	if run == nil {
		return nil
	}
	return &model.Run{
		IssueID:           run.IssueId,
		RunID:             run.RunId,
		Status:            protoStatusToModel(run.Status),
		Agent:             run.Agent,
		Model:             run.Model,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		PRUrl:             run.PrUrl,
		PRNumber:          int(run.PrNumber),
		PRState:           run.PrState,
		StartedAt:         time.Unix(run.StartedAtUnix, 0),
		UpdatedAt:         time.Unix(run.UpdatedAtUnix, 0),
		SessionName:       run.SessionName,
		Multiplexer:       protoToMultiplexer(run.Multiplexer),
		ServerPort:        int(run.ServerPort),
		OpenCodeSessionID: run.OpencodeSessionId,
		ContinuedFrom:     run.ContinuedFrom,
	}
}

func modelIssueToProto(issue *model.Issue) *orchpb.Issue {
	if issue == nil {
		return nil
	}
	return &orchpb.Issue{
		Id:             sanitizeUTF8(issue.ID),
		Title:          sanitizeUTF8(issue.Title),
		Topic:          sanitizeUTF8(issue.Topic),
		Summary:        sanitizeUTF8(issue.Summary),
		Status:         modelIssueStatusToProto(issue.Status),
		Tags:           sanitizeUTF8Slice(issue.Tags),
		Body:           sanitizeUTF8(issue.Body),
		Path:           sanitizeUTF8(issue.Path),
		ModifiedAtUnix: issue.ModifiedAt.Unix(),
	}
}

func protoIssueToModel(issue *orchpb.Issue) *model.Issue {
	if issue == nil {
		return nil
	}
	return &model.Issue{
		ID:         issue.Id,
		Title:      issue.Title,
		Topic:      issue.Topic,
		Summary:    issue.Summary,
		Status:     protoIssueStatusToModel(issue.Status),
		Tags:       issue.Tags,
		Body:       issue.Body,
		Path:       issue.Path,
		ModifiedAt: time.Unix(issue.ModifiedAtUnix, 0),
	}
}

func modelEventToProto(event *model.Event) *orchpb.Event {
	if event == nil {
		return nil
	}
	return &orchpb.Event{
		TimestampUnix: event.Timestamp.Unix(),
		Type:          sanitizeUTF8(string(event.Type)),
		Name:          sanitizeUTF8(event.Name),
		Attrs:         sanitizeUTF8Map(event.Attrs),
	}
}

func protoEventToModel(event *orchpb.Event) *model.Event {
	if event == nil {
		return nil
	}
	return &model.Event{
		Timestamp: time.Unix(event.TimestampUnix, 0),
		Type:      model.EventType(event.Type),
		Name:      event.Name,
		Attrs:     event.Attrs,
	}
}

func protoRunStatusSliceToModel(statuses []orchpb.RunStatus) []model.Status {
	result := make([]model.Status, len(statuses))
	for i, s := range statuses {
		result[i] = protoStatusToModel(s)
	}
	return result
}

func protoIssueStatusSliceToModel(statuses []orchpb.IssueStatus) []model.IssueStatus {
	result := make([]model.IssueStatus, len(statuses))
	for i, s := range statuses {
		result[i] = protoIssueStatusToModel(s)
	}
	return result
}
