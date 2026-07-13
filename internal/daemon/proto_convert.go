package daemon

import (
	"fmt"
	"time"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/sessionlifecycle"
)

func modelStatusToProto(s model.Status) (orchpb.RunStatus, error) {
	switch s {
	case model.StatusQueued:
		return orchpb.RunStatus_RUN_STATUS_QUEUED, nil
	case model.StatusBooting:
		return orchpb.RunStatus_RUN_STATUS_BOOTING, nil
	case model.StatusRunning:
		return orchpb.RunStatus_RUN_STATUS_RUNNING, nil
	case model.StatusWaiting:
		return orchpb.RunStatus_RUN_STATUS_WAITING, nil
	case model.StatusRateLimited:
		return orchpb.RunStatus_RUN_STATUS_RATE_LIMITED, nil
	case model.StatusPROpen:
		return orchpb.RunStatus_RUN_STATUS_PR_OPEN, nil
	case model.StatusDone:
		return orchpb.RunStatus_RUN_STATUS_DONE, nil
	case model.StatusFailed:
		return orchpb.RunStatus_RUN_STATUS_FAILED, nil
	case model.StatusCanceled:
		return orchpb.RunStatus_RUN_STATUS_CANCELED, nil
	case model.StatusUnknown:
		return orchpb.RunStatus_RUN_STATUS_UNKNOWN, nil
	default:
		return orchpb.RunStatus_RUN_STATUS_UNSPECIFIED, fmt.Errorf("unknown model run status: %q", s)
	}
}

func protoStatusToModel(s orchpb.RunStatus) (model.Status, error) {
	switch s {
	case orchpb.RunStatus_RUN_STATUS_QUEUED:
		return model.StatusQueued, nil
	case orchpb.RunStatus_RUN_STATUS_BOOTING:
		return model.StatusBooting, nil
	case orchpb.RunStatus_RUN_STATUS_RUNNING:
		return model.StatusRunning, nil
	case orchpb.RunStatus_RUN_STATUS_WAITING:
		return model.StatusWaiting, nil
	case orchpb.RunStatus_RUN_STATUS_RATE_LIMITED:
		return model.StatusRateLimited, nil
	case orchpb.RunStatus_RUN_STATUS_PR_OPEN:
		return model.StatusPROpen, nil
	case orchpb.RunStatus_RUN_STATUS_DONE:
		return model.StatusDone, nil
	case orchpb.RunStatus_RUN_STATUS_FAILED:
		return model.StatusFailed, nil
	case orchpb.RunStatus_RUN_STATUS_CANCELED:
		return model.StatusCanceled, nil
	case orchpb.RunStatus_RUN_STATUS_UNKNOWN:
		return model.StatusUnknown, nil
	default:
		return "", fmt.Errorf("unknown proto run status: %s", s.String())
	}
}

func modelIssueStatusToProto(s model.IssueStatus) (orchpb.IssueStatus, error) {
	switch s {
	case model.IssueStatusOpen:
		return orchpb.IssueStatus_ISSUE_STATUS_OPEN, nil
	case model.IssueStatusResolved:
		return orchpb.IssueStatus_ISSUE_STATUS_RESOLVED, nil
	case model.IssueStatusClosed:
		return orchpb.IssueStatus_ISSUE_STATUS_CLOSED, nil
	default:
		return orchpb.IssueStatus_ISSUE_STATUS_UNSPECIFIED, fmt.Errorf("unknown model issue status: %q", s)
	}
}

func protoIssueStatusToModel(s orchpb.IssueStatus) (model.IssueStatus, error) {
	switch s {
	case orchpb.IssueStatus_ISSUE_STATUS_OPEN:
		return model.IssueStatusOpen, nil
	case orchpb.IssueStatus_ISSUE_STATUS_RESOLVED:
		return model.IssueStatusResolved, nil
	case orchpb.IssueStatus_ISSUE_STATUS_CLOSED:
		return model.IssueStatusClosed, nil
	default:
		return "", fmt.Errorf("unknown proto issue status: %s", s.String())
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

func modelRunToProto(run *model.Run) (*orchpb.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := modelStatusToProto(run.Status)
	if err != nil {
		return nil, fmt.Errorf("convert run %s#%s status: %w", run.IssueID, run.RunID, err)
	}
	sessionlifecycle.Apply(run)

	protoRun := &orchpb.Run{
		IssueId:                sanitizeUTF8(string(run.IssueID)),
		RunId:                  sanitizeUTF8(string(run.RunID)),
		Status:                 status,
		Agent:                  sanitizeUTF8(run.Agent),
		Profile:                sanitizeUTF8(run.Profile),
		Model:                  sanitizeUTF8(run.Model),
		Branch:                 sanitizeUTF8(run.Branch),
		WorktreePath:           sanitizeUTF8(run.WorktreePath),
		Target:                 sanitizeUTF8(run.Target),
		TargetHost:             sanitizeUTF8(run.TargetHost),
		PrUrl:                  sanitizeUTF8(run.PRUrl),
		StartedAtUnix:          run.StartedAt.Unix(),
		UpdatedAtUnix:          run.UpdatedAt.Unix(),
		ElapsedSeconds:         int32(run.UpdatedAt.Sub(run.StartedAt).Seconds()),
		SessionName:            sanitizeUTF8(run.SessionName),
		Multiplexer:            multiplexerToProto(run.Multiplexer),
		ServerPort:             int32(run.ServerPort),
		OpencodeSessionId:      sanitizeUTF8(run.OpenCodeSessionID),
		ContinuedFrom:          sanitizeUTF8(run.ContinuedFrom),
		PrNumber:               int32(run.PRNumber),
		PrState:                sanitizeUTF8(run.PRState),
		Alive:                  run.Alive,
		AliveKnown:             run.AliveKnown,
		WorktreeExists:         run.WorktreeExists,
		AgentSessionId:         sanitizeUTF8(run.AgentSessionID),
		AgentSessionGeneration: int32(run.AgentSessionGeneration),
		SessionState:           sanitizeUTF8(string(run.SessionState)),
		SessionStateDetail:     sanitizeUTF8(run.SessionStateDetail),
	}

	return protoRun, nil
}

func populateRunDisplayFields(run *orchpb.Run) {
	if run == nil {
		return
	}
	run.StatusDisplay = protoEnumDisplayString(protoRunStatusToString(run.Status))
	run.MultiplexerName = protoDisplayString(protoMultiplexerToString(run.Multiplexer))
	run.BranchStateDisplay = protoDisplayString(protoBranchStateToString(run.BranchState))
}

func protoEnumDisplayString(value string, err error) string {
	if err != nil {
		return ""
	}
	return protoDisplayString(value)
}

func protoDisplayString(value string) string {
	if value == "" || value == "unknown" {
		return ""
	}
	return sanitizeUTF8(value)
}

func protoRunToModel(run *orchpb.Run) (*model.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := protoStatusToModel(run.Status)
	if err != nil {
		return nil, fmt.Errorf("convert run %s#%s status: %w", run.IssueId, run.RunId, err)
	}
	return &model.Run{
		IssueID:                model.IssueID(run.IssueId),
		RunID:                  model.RunID(run.RunId),
		Status:                 status,
		Agent:                  run.Agent,
		Profile:                run.Profile,
		Model:                  run.Model,
		Branch:                 run.Branch,
		WorktreePath:           run.WorktreePath,
		Target:                 run.Target,
		TargetHost:             run.TargetHost,
		PRUrl:                  run.PrUrl,
		PRNumber:               int(run.PrNumber),
		PRState:                run.PrState,
		StartedAt:              time.Unix(run.StartedAtUnix, 0),
		UpdatedAt:              time.Unix(run.UpdatedAtUnix, 0),
		SessionName:            run.SessionName,
		Multiplexer:            protoToMultiplexer(run.Multiplexer),
		ServerPort:             int(run.ServerPort),
		OpenCodeSessionID:      run.OpencodeSessionId,
		ContinuedFrom:          run.ContinuedFrom,
		AgentSessionID:         run.AgentSessionId,
		AgentSessionGeneration: int(run.AgentSessionGeneration),
		SessionState:           model.SessionState(run.SessionState),
		SessionStateDetail:     run.SessionStateDetail,
	}, nil
}

func modelIssueToProto(issue *model.Issue) (*orchpb.Issue, error) {
	if issue == nil {
		return nil, nil
	}
	status, err := modelIssueStatusToProto(issue.Status)
	if err != nil {
		return nil, fmt.Errorf("convert issue %s status: %w", issue.ID, err)
	}
	protoIssue := &orchpb.Issue{
		Id:             sanitizeUTF8(string(issue.ID)),
		Title:          sanitizeUTF8(issue.Title),
		Topic:          sanitizeUTF8(issue.Topic),
		Summary:        sanitizeUTF8(issue.Summary),
		Status:         status,
		Tags:           sanitizeUTF8Slice(issue.Tags),
		Body:           sanitizeUTF8(issue.Body),
		Path:           sanitizeUTF8(issue.Path),
		ModifiedAtUnix: issue.ModifiedAt.Unix(),
		BaseBranch:     sanitizeUTF8(issue.BaseBranch),
	}
	populateIssueDisplayFields(protoIssue)
	return protoIssue, nil
}

func populateIssueDisplayFields(issue *orchpb.Issue) {
	if issue == nil {
		return
	}
	issue.StatusDisplay = protoEnumDisplayString(protoIssueStatusToString(issue.Status))
}

func protoIssueToModel(issue *orchpb.Issue) (*model.Issue, error) {
	if issue == nil {
		return nil, nil
	}
	status, err := protoIssueStatusToModel(issue.Status)
	if err != nil {
		return nil, fmt.Errorf("convert issue %s status: %w", issue.Id, err)
	}
	return &model.Issue{
		ID:         model.IssueID(issue.Id),
		Title:      issue.Title,
		Topic:      issue.Topic,
		Summary:    issue.Summary,
		Status:     status,
		Tags:       issue.Tags,
		Body:       issue.Body,
		Path:       issue.Path,
		BaseBranch: issue.BaseBranch,
		ModifiedAt: time.Unix(issue.ModifiedAtUnix, 0),
	}, nil
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

func protoRunStatusSliceToModel(statuses []orchpb.RunStatus) ([]model.Status, error) {
	result := make([]model.Status, len(statuses))
	for i, s := range statuses {
		status, err := protoStatusToModel(s)
		if err != nil {
			return nil, err
		}
		result[i] = status
	}
	return result, nil
}

func protoIssueStatusSliceToModel(statuses []orchpb.IssueStatus) ([]model.IssueStatus, error) {
	result := make([]model.IssueStatus, len(statuses))
	for i, s := range statuses {
		status, err := protoIssueStatusToModel(s)
		if err != nil {
			return nil, err
		}
		result[i] = status
	}
	return result, nil
}
