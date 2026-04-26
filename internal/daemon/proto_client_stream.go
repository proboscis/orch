package daemon

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"google.golang.org/protobuf/proto"
)

// RunEventStream wraps a long-lived subscription to the daemon's run event
// stream. The Events channel emits frames until the context is canceled or
// the daemon closes the connection. Close releases the underlying socket.
type RunEventStream struct {
	events chan *orchpb.RunEventFrame
	conn   net.Conn
	cancel context.CancelFunc
	doneCh chan struct{}
	errMu  sync.Mutex
	err    error
}

func (s *RunEventStream) Events() <-chan *orchpb.RunEventFrame { return s.events }

// Err returns the terminal error after Events has closed, or nil for clean
// EOF / context cancellation.
func (s *RunEventStream) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *RunEventStream) setErr(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// Close terminates the stream. Safe to call multiple times.
func (s *RunEventStream) Close() error {
	s.cancel()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	<-s.doneCh
	return nil
}

// StreamRunEvents opens a dedicated connection to the daemon and subscribes
// to run state transition events. The first frame on the wire is an
// StreamRunEventsAck — once received, subsequent frames carry RunEventFrame
// payloads. Returns synchronously after the ack is received.
func (c *ProtoClient) StreamRunEvents(ctx context.Context, filter *orchpb.StreamRunEventsRequest) (*RunEventStream, error) {
	if filter == nil {
		filter = &orchpb.StreamRunEventsRequest{}
	}
	if filter.Context == nil {
		filter.Context = c.requestContext(c.projectRoot)
	}

	network, address, err := c.dialTarget()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon (%s %s): %w", network, address, err)
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_StreamRunEvents{StreamRunEvents: filter},
	}
	if err := writeProtoRequestFrame(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send stream request: %w", err)
	}

	ack, err := readProtoResponseFrame(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read stream ack: %w", err)
	}
	if !ack.Ok {
		conn.Close()
		return nil, fmt.Errorf("daemon rejected stream: %s", ack.Error)
	}
	if _, ok := ack.Response.(*orchpb.Response_StreamRunEventsAck); !ok {
		conn.Close()
		return nil, fmt.Errorf("expected StreamRunEventsAck, got %T", ack.Response)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &RunEventStream{
		events: make(chan *orchpb.RunEventFrame, 64),
		conn:   conn,
		cancel: cancel,
		doneCh: make(chan struct{}),
	}

	go stream.readLoop(streamCtx)
	return stream, nil
}

func (s *RunEventStream) readLoop(ctx context.Context) {
	defer close(s.doneCh)
	defer close(s.events)

	go func() {
		<-ctx.Done()
		_ = s.conn.Close()
	}()

	for {
		resp, err := readProtoResponseFrame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			if ctx.Err() != nil {
				return
			}
			s.setErr(err)
			return
		}
		if !resp.Ok {
			s.setErr(fmt.Errorf("daemon error: %s", resp.Error))
			return
		}
		evWrap, ok := resp.Response.(*orchpb.Response_RunEvent)
		if !ok || evWrap.RunEvent == nil {
			continue
		}
		select {
		case s.events <- evWrap.RunEvent:
		case <-ctx.Done():
			return
		}
	}
}

func writeProtoRequestFrame(conn net.Conn, req *orchpb.Request) error {
	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	if _, err := conn.Write(data); err != nil {
		return err
	}
	return nil
}

func readProtoResponseFrame(conn net.Conn) (*orchpb.Response, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > maxProtoMessageSize {
		return nil, fmt.Errorf("response too large: %d bytes", msgLen)
	}
	buf := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	var resp orchpb.Response
	if err := proto.Unmarshal(buf, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
