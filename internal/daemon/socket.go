package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

const (
	socketFile = "daemon.sock"
)

func SocketFilePath(vaultPath string) string {
	return filepath.Join(OrchDir(vaultPath), socketFile)
}

type SendRequest struct {
	Type      string `json:"type"`
	IssueID   string `json:"issue_id"`
	RunID     string `json:"run_id"`
	Message   string `json:"message"`
	NoEnter   bool   `json:"no_enter,omitempty"`
	VaultPath string `json:"vault_path"`
}

type SendResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type SocketServer struct {
	vaultPath string
	store     store.Store
	listener  net.Listener
	logger    Logger
	stopCh    chan struct{}
	stopOnce  sync.Once
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func NewSocketServer(vaultPath string, st store.Store, logger Logger) *SocketServer {
	return &SocketServer{
		vaultPath: vaultPath,
		store:     st,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
}

func (s *SocketServer) Start() error {
	socketPath := SocketFilePath(s.vaultPath)

	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	if err := os.Chmod(socketPath, 0660); err != nil {
		s.logger.Printf("warning: failed to chmod socket: %v", err)
	}

	s.logger.Printf("socket server listening on %s", socketPath)

	go s.acceptLoop()

	return nil
}

func (s *SocketServer) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		if s.listener != nil {
			s.listener.Close()
		}
		os.Remove(SocketFilePath(s.vaultPath))
	})
}

func (s *SocketServer) acceptLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				s.logger.Printf("accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *SocketServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req SendRequest
	if err := decoder.Decode(&req); err != nil {
		s.logger.Printf("failed to decode request: %v", err)
		encoder.Encode(SendResponse{OK: false, Error: "invalid request"})
		return
	}

	switch req.Type {
	case "send":
		s.handleSend(req, encoder)
	default:
		encoder.Encode(SendResponse{OK: false, Error: "unknown request type"})
	}
}

func (s *SocketServer) handleSend(req SendRequest, encoder *json.Encoder) {
	encoder.Encode(SendResponse{OK: true})
	go s.processSend(req)
}

func (s *SocketServer) processSend(req SendRequest) {
	s.logger.Printf("processing send for %s#%s", req.IssueID, req.RunID)

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := s.store.GetRun(ref)
	if err != nil {
		s.logger.Printf("failed to get run %s#%s: %v", req.IssueID, req.RunID, err)
		return
	}

	if run.Agent != string(agent.AgentOpenCode) {
		s.logger.Printf("run %s#%s is not opencode agent, skipping", req.IssueID, req.RunID)
		return
	}

	if run.ServerPort <= 0 || run.OpenCodeSessionID == "" {
		s.logger.Printf("run %s#%s missing server config (port=%d, session=%s)",
			req.IssueID, req.RunID, run.ServerPort, run.OpenCodeSessionID)
		return
	}

	client := agent.NewOpenCodeClient(run.ServerPort)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err = client.SendMessagePrompt(ctx, run.OpenCodeSessionID, req.Message, run.WorktreePath)
	if err != nil {
		s.logger.Printf("failed to send message to %s#%s: %v", req.IssueID, req.RunID, err)
		return
	}

	s.logger.Printf("message sent successfully to %s#%s", req.IssueID, req.RunID)
}

func SendViaDaemon(vaultPath string, run *model.Run, message string, noEnter bool) error {
	socketPath := SocketFilePath(vaultPath)

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := SendRequest{
		Type:      "send",
		IssueID:   run.IssueID,
		RunID:     run.RunID,
		Message:   message,
		NoEnter:   noEnter,
		VaultPath: vaultPath,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	var resp SendResponse
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func IsDaemonSocketAvailable(vaultPath string) bool {
	if !IsRunning(vaultPath) {
		return false
	}
	socketPath := SocketFilePath(vaultPath)
	_, err := os.Stat(socketPath)
	return err == nil
}
