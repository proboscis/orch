package orchapi

import (
	"context"
	"fmt"
	"sync"
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
	mu     sync.Mutex
	err    error
}

func (s *daemonRunEventStream) Events() <-chan *RunEvent { return s.events }
func (s *daemonRunEventStream) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.raw.Err()
}
func (s *daemonRunEventStream) Close() error {
	err := s.raw.Close()
	<-s.done
	return err
}

func (s *daemonRunEventStream) translateLoop() {
	defer close(s.events)
	defer close(s.done)
	for ev := range s.raw.Events() {
		domainEvent, err := protoEventToOrchAPI(ev)
		if err != nil {
			s.setErr(err)
			return
		}
		s.events <- domainEvent
	}
}

func (s *daemonRunEventStream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (c *DaemonClient) StreamRunEvents(ctx context.Context, filter *RunEventFilter) (RunEventStream, error) {
	pbReq := &orchpb.StreamRunEventsRequest{}
	if filter != nil {
		pbReq.IssueId = string(filter.IssueID)
		pbReq.RunId = string(filter.RunID)
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

func protoEventToOrchAPI(ev *orchpb.RunEventFrame) (*RunEvent, error) {
	if ev == nil {
		return nil, fmt.Errorf("nil run event frame")
	}

	projectID, err := model.NewProjectID(ev.ProjectId)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id in run event %q: %w", ev.ProjectId, err)
	}

	return &RunEvent{
		Timestamp: time.UnixMilli(ev.TimestampUnixMs),
		IssueID:   model.IssueID(ev.IssueId),
		RunID:     model.RunID(ev.RunId),
		ShortID:   model.ShortID(ev.ShortId),
		From:      protoRunStatusToDomain(ev.FromStatus),
		To:        protoRunStatusToDomain(ev.ToStatus),
		Source:    ev.Source,
		ProjectID: projectID,
	}, nil
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
