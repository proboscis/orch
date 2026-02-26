package daemon

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

func writeProtoRequest(t *testing.T, conn net.Conn, req *orchpb.Request) {
	t.Helper()

	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := conn.Write(lenBuf); err != nil {
		t.Fatalf("write request length: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write request body: %v", err)
	}
}

func readProtoResponse(t *testing.T, conn net.Conn) *orchpb.Response {
	t.Helper()

	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		t.Fatalf("read response length: %v", err)
	}

	respLen := binary.BigEndian.Uint32(lenBuf)
	respData := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respData); err != nil {
		t.Fatalf("read response body: %v", err)
	}

	resp := &orchpb.Response{}
	if err := proto.Unmarshal(respData, resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func pingRequest() *orchpb.Request {
	return &orchpb.Request{Request: &orchpb.Request_Ping{Ping: &orchpb.PingRequest{}}}
}

func TestHandleProtoConnectionServesMultipleRequestsOnSingleConn(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	srv := &SocketServer{logger: log.New(io.Discard, "", 0)}
	go srv.handleProtoConnection(serverConn)

	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}

	writeProtoRequest(t, clientConn, pingRequest())
	resp1 := readProtoResponse(t, clientConn)
	if !resp1.GetPing().GetOk() {
		t.Fatalf("first ping response not ok: %+v", resp1)
	}

	writeProtoRequest(t, clientConn, pingRequest())
	resp2 := readProtoResponse(t, clientConn)
	if !resp2.GetPing().GetOk() {
		t.Fatalf("second ping response not ok: %+v", resp2)
	}
}

func TestProtoClientPingReusesSingleSocketConnection(t *testing.T) {
	runtimeRoot, err := os.MkdirTemp("", "orchrt-")
	if err != nil {
		t.Fatalf("mktemp runtime root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeRoot)

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("ensure runtime dir: %v", err)
	}

	socketPath := xdg.SocketPath()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	srv := &SocketServer{logger: log.New(io.Discard, "", 0)}
	var acceptCount atomic.Int32

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			acceptCount.Add(1)
			go srv.handleProtoConnection(conn)
		}
	}()

	client := NewProtoClient("/tmp/project")
	if err := client.Ping(); err != nil {
		t.Fatalf("first ping failed: %v", err)
	}
	if err := client.Ping(); err != nil {
		t.Fatalf("second ping failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if got := acceptCount.Load(); got != 1 {
		t.Fatalf("accept count = %d, want 1 (connection should be reused)", got)
	}

	client.mu.Lock()
	client.resetConnLocked()
	client.mu.Unlock()

	_ = listener.Close()
	<-acceptDone
}

func TestProtoClientIsAvailableRemoteReusesSingleTCPConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer listener.Close()

	srv := &SocketServer{logger: log.New(io.Discard, "", 0)}
	var acceptCount atomic.Int32

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			acceptCount.Add(1)
			go srv.handleProtoConnection(conn)
		}
	}()

	client := NewProtoClientWithAddress("/tmp/project", "", listener.Addr().String())
	if !client.IsAvailable() {
		t.Fatalf("first IsAvailable() = false, want true")
	}
	if !client.IsAvailable() {
		t.Fatalf("second IsAvailable() = false, want true")
	}

	time.Sleep(100 * time.Millisecond)
	if got := acceptCount.Load(); got != 1 {
		t.Fatalf("accept count = %d, want 1 (connection should be reused)", got)
	}

	_ = client.Close()
	_ = listener.Close()
	<-acceptDone
}
