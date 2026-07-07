package daemon

import (
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/proboscis/orch/api/orchpb"
)

// TestHandleProtoStreamRunEvents_AckThenForwardsFrames exercises the streaming
// dispatch path end-to-end: the server must answer the StreamRunEventsRequest
// with an Ack and then push every published RunEventFrame to the client
// connection until the client disconnects.
func TestHandleProtoStreamRunEvents_AckThenForwardsFrames(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	srv := NewSocketServer(nil, log.New(io.Discard, "", 0))
	go srv.handleProtoConnection(serverConn)

	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}

	streamReq := &orchpb.Request{
		Request: &orchpb.Request_StreamRunEvents{
			StreamRunEvents: &orchpb.StreamRunEventsRequest{},
		},
	}
	writeProtoRequest(t, clientConn, streamReq)

	ack := readProtoResponse(t, clientConn)
	if !ack.GetOk() {
		t.Fatalf("expected ok ack, got error %q", ack.GetError())
	}
	if ack.GetStreamRunEventsAck() == nil {
		t.Fatalf("expected StreamRunEventsAck, got %T", ack.Response)
	}

	// Wait until the server-side subscription is registered before publishing,
	// otherwise the event would be dropped before the subscriber attaches.
	deadline := time.Now().Add(time.Second)
	for srv.RunEventBus().SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscription was never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	srv.PublishRunEvent(&orchpb.RunEventFrame{
		IssueId:    "issue-1",
		RunId:      "run-1",
		FromStatus: orchpb.RunStatus_RUN_STATUS_RUNNING,
		ToStatus:   orchpb.RunStatus_RUN_STATUS_WAITING,
		Source:     "daemon",
	})

	frame := readProtoResponse(t, clientConn)
	if !frame.GetOk() {
		t.Fatalf("expected ok frame, got error %q", frame.GetError())
	}
	ev := frame.GetRunEvent()
	if ev == nil {
		t.Fatalf("expected RunEvent payload, got %T", frame.Response)
	}
	if ev.IssueId != "issue-1" || ev.RunId != "run-1" {
		t.Fatalf("unexpected event ids: %+v", ev)
	}
	if ev.FromStatus != orchpb.RunStatus_RUN_STATUS_RUNNING || ev.ToStatus != orchpb.RunStatus_RUN_STATUS_WAITING {
		t.Fatalf("unexpected transition: %v -> %v", ev.FromStatus, ev.ToStatus)
	}
}

// TestHandleProtoStreamRunEvents_RespectsFilter ensures the server only sends
// frames matching the subscriber's issue/run filters.
func TestHandleProtoStreamRunEvents_RespectsFilter(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	srv := NewSocketServer(nil, log.New(io.Discard, "", 0))
	go srv.handleProtoConnection(serverConn)

	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_StreamRunEvents{
			StreamRunEvents: &orchpb.StreamRunEventsRequest{IssueId: "match-me"},
		},
	}
	writeProtoRequest(t, clientConn, req)

	ack := readProtoResponse(t, clientConn)
	if !ack.GetOk() || ack.GetStreamRunEventsAck() == nil {
		t.Fatalf("missing ack: %+v", ack)
	}

	deadline := time.Now().Add(time.Second)
	for srv.RunEventBus().SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscription was never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Skipped: filter mismatch.
	srv.PublishRunEvent(&orchpb.RunEventFrame{IssueId: "other", RunId: "r"})
	// Delivered: filter match.
	srv.PublishRunEvent(&orchpb.RunEventFrame{IssueId: "match-me", RunId: "r"})

	frame := readProtoResponse(t, clientConn)
	if frame.GetRunEvent() == nil || frame.GetRunEvent().IssueId != "match-me" {
		t.Fatalf("expected match-me event, got %+v", frame)
	}
}
