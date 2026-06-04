package orchapi

import (
	"context"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/model"
)

// daemonRunEventStream adapts a daemon.RunEventStream (proto-typed) into the
// orchapi.RunEventStream interface (domain-typed).
type daemonRunEventStream struct {
	raw    *daemon.RunEventStream
	events chan *RunEvent
	done   chan struct{}
}

func (s *daemonRunEventStream) Events() <-chan *RunEvent { return s.events }
func (s *daemonRunEventStream) Err() error               { return s.raw.Err() }
func (s *daemonRunEventStream) Close() error {
	err := s.raw.Close()
	<-s.done
	return err
}

func (s *daemonRunEventStream) translateLoop() {
	defer close(s.events)
	defer close(s.done)
	for ev := range s.raw.Events() {
		s.events <- protoEventToOrchAPI(ev)
	}
}

func (c *DaemonClient) StreamRunEvents(ctx context.Context, filter *RunEventFilter) (RunEventStream, error) {
	pbReq := &orchpb.StreamRunEventsRequest{}
	if filter != nil {
		pbReq.IssueId = filter.IssueID.String()
		pbReq.RunId = filter.RunID.String()
	}

	raw, err := c.proto.StreamRunEvents(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	stream := &daemonRunEventStream{
		raw:    raw,
		events: make(chan *RunEvent, 64),
		done:   make(chan struct{}),
	}
	go stream.translateLoop()
	return stream, nil
}

func protoEventToOrchAPI(ev *orchpb.RunEventFrame) *RunEvent {
	if ev == nil {
		return nil
	}
	var projectID model.ProjectID
	if ev.ProjectId != "" {
		if id, err := model.NewNormalizedProjectID(ev.ProjectId); err == nil {
			projectID = id
		}
	}
	return &RunEvent{
		Timestamp: time.UnixMilli(ev.TimestampUnixMs),
		IssueID:   model.NewIssueID(ev.IssueId),
		RunID:     model.NewRunID(ev.RunId),
		ShortID:   model.NewShortID(ev.ShortId),
		From:      protoRunStatusToDomain(ev.FromStatus),
		To:        protoRunStatusToDomain(ev.ToStatus),
		Source:    ev.Source,
		ProjectID: projectID,
	}
}

func protoRunStatusToDomain(s orchpb.RunStatus) RunStatus {
	switch s {
	case orchpb.RunStatus_RUN_STATUS_QUEUED:
		return RunStatusQueued
	case orchpb.RunStatus_RUN_STATUS_BOOTING:
		return RunStatusBooting
	case orchpb.RunStatus_RUN_STATUS_RUNNING:
		return RunStatusRunning
	case orchpb.RunStatus_RUN_STATUS_WAITING:
		return RunStatusWaiting
	case orchpb.RunStatus_RUN_STATUS_RATE_LIMITED:
		return RunStatusRateLimited
	case orchpb.RunStatus_RUN_STATUS_PR_OPEN:
		return RunStatusPROpen
	case orchpb.RunStatus_RUN_STATUS_DONE:
		return RunStatusDone
	case orchpb.RunStatus_RUN_STATUS_FAILED:
		return RunStatusFailed
	case orchpb.RunStatus_RUN_STATUS_CANCELED:
		return RunStatusCanceled
	default:
		return RunStatus("")
	}
}
