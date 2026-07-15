package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type restartDaemonMetadata struct {
	StartedAt  time.Time `json:"started_at"`
	ListenAddr *string   `json:"listen_addr"`
}

type daemonRestartSandbox struct {
	env         []string
	runtimeRoot string
	stateRoot   string
}

func newDaemonRestartSandbox(t *testing.T) *daemonRestartSandbox {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "orch-restart-")
	if err != nil {
		t.Fatalf("create daemon restart sandbox: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	runtimeRoot := filepath.Join(root, "runtime")
	stateRoot := filepath.Join(root, "state")
	dataRoot := filepath.Join(root, "data")
	configRoot := filepath.Join(root, "config")
	homeRoot := filepath.Join(root, "home")
	for _, dir := range []string{runtimeRoot, stateRoot, dataRoot, configRoot, homeRoot} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("create daemon restart sandbox directory %s: %v", dir, err)
		}
	}

	env := make([]string, 0, len(os.Environ())+7)
	for _, entry := range os.Environ() {
		switch {
		case strings.HasPrefix(entry, "HOME="),
			strings.HasPrefix(entry, "XDG_RUNTIME_DIR="),
			strings.HasPrefix(entry, "XDG_STATE_HOME="),
			strings.HasPrefix(entry, "XDG_DATA_HOME="),
			strings.HasPrefix(entry, "XDG_CONFIG_HOME="),
			strings.HasPrefix(entry, "ORCH_REMOTE="),
			strings.HasPrefix(entry, "ORCH_PROJECT="):
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+homeRoot,
		"XDG_RUNTIME_DIR="+runtimeRoot,
		"XDG_STATE_HOME="+stateRoot,
		"XDG_DATA_HOME="+dataRoot,
		"XDG_CONFIG_HOME="+configRoot,
		"ORCH_REMOTE=",
		"ORCH_PROJECT=",
	)

	sandbox := &daemonRestartSandbox{env: env, runtimeRoot: runtimeRoot, stateRoot: stateRoot}
	t.Cleanup(func() {
		_, _ = sandbox.run("daemon", "kill")
	})
	return sandbox
}

func (s *daemonRestartSandbox) run(args ...string) (string, error) {
	cmdArgs := append([]string{"--remote="}, args...)
	cmd := newOrchCommand(cmdArgs...)
	cmd.Env = s.env
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (s *daemonRestartSandbox) metadata() (*restartDaemonMetadata, error) {
	path := filepath.Join(s.runtimeRoot, "orch", "daemon.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var metadata restartDaemonMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func (s *daemonRestartSandbox) waitForRunning(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		lastOutput, lastErr = s.run("daemon", "status")
		if lastErr == nil && strings.Contains(lastOutput, "Status: running") {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become ready: last error=%v output=%s", lastErr, lastOutput)
}

func (s *daemonRestartSandbox) startPlainDaemon(t *testing.T) {
	t.Helper()
	cmd := newOrchCommand("--remote=", "daemon", "run", "--listen=")
	cmd.Env = s.env
	if err := cmd.Start(); err != nil {
		t.Fatalf("start plain daemon: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-done:
			return
		default:
		}
		_, _ = s.run("daemon", "kill")
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	if err := s.waitForRunning(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func (s *daemonRestartSandbox) overwriteRecordedListenAddr(t *testing.T, listenAddr string) {
	t.Helper()
	path := filepath.Join(s.runtimeRoot, "orch", "daemon.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daemon metadata for rewrite: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode daemon metadata for rewrite: %v", err)
	}
	metadata["listen_addr"] = listenAddr
	data, err = json.Marshal(metadata)
	if err != nil {
		t.Fatalf("encode daemon metadata rewrite: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write daemon metadata rewrite: %v", err)
	}
}

func (s *daemonRestartSandbox) waitForTCP(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("tcp listener %s did not become ready: %w", address, lastErr)
}

func TestDaemonRestartPreservesMasterListenAddress(t *testing.T) {
	sandbox := newDaemonRestartSandbox(t)
	listenAddr, err := reserveLoopbackTCPAddr()
	if err != nil {
		t.Fatalf("reserve master listen address: %v", err)
	}

	startOutput, err := sandbox.run("master", "start", "--listen", "tcp://"+listenAddr)
	if err != nil {
		t.Fatalf("start master: %v\n%s", err, startOutput)
	}
	if err := sandbox.waitForTCP(listenAddr, 5*time.Second); err != nil {
		statusOutput, statusErr := sandbox.run("daemon", "status")
		metadata, metadataErr := os.ReadFile(filepath.Join(sandbox.runtimeRoot, "orch", "daemon.json"))
		logOutput, logErr := os.ReadFile(filepath.Join(sandbox.stateRoot, "orch", "daemon.log"))
		t.Fatalf("%v\nstart output: %s\nstatus: err=%v output=%s\nmetadata: err=%v content=%s\nlog: err=%v content=%s",
			err, startOutput, statusErr, statusOutput, metadataErr, metadata, logErr, logOutput)
	}
	before, err := sandbox.metadata()
	if err != nil {
		t.Fatalf("read metadata before restart: %v", err)
	}

	if output, err := sandbox.run("daemon-restart"); err != nil {
		t.Fatalf("restart master: %v\n%s", err, output)
	}
	after, err := sandbox.metadata()
	if err != nil {
		t.Fatalf("read metadata after restart: %v", err)
	}
	if !after.StartedAt.After(before.StartedAt) {
		t.Fatalf("daemon metadata was not refreshed: before=%s after=%s", before.StartedAt, after.StartedAt)
	}
	if after.ListenAddr == nil || *after.ListenAddr != "tcp://"+listenAddr {
		t.Fatalf("listen address was not preserved in runtime metadata: got=%v want=%q", after.ListenAddr, "tcp://"+listenAddr)
	}
	if err := sandbox.waitForTCP(listenAddr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if output, err := sandbox.run("daemon", "status"); err != nil {
		t.Fatalf("daemon status after restart: %v\n%s", err, output)
	} else if !strings.Contains(output, "Status: running") {
		t.Fatalf("daemon status did not report running after restart:\n%s", output)
	}
}

func TestDaemonRestartPreservesPlainMode(t *testing.T) {
	sandbox := newDaemonRestartSandbox(t)
	sandbox.startPlainDaemon(t)

	before, err := sandbox.metadata()
	if err != nil {
		t.Fatalf("read plain daemon metadata before restart: %v", err)
	}
	if before.ListenAddr == nil || *before.ListenAddr != "" {
		t.Fatalf("plain daemon did not record disabled TCP listener: %v", before.ListenAddr)
	}

	if output, err := sandbox.run("daemon-restart"); err != nil {
		t.Fatalf("restart plain daemon: %v\n%s", err, output)
	}
	after, err := sandbox.metadata()
	if err != nil {
		t.Fatalf("read plain daemon metadata after restart: %v", err)
	}
	if !after.StartedAt.After(before.StartedAt) {
		t.Fatalf("plain daemon metadata was not refreshed: before=%s after=%s", before.StartedAt, after.StartedAt)
	}
	if after.ListenAddr == nil || *after.ListenAddr != "" {
		t.Fatalf("plain daemon restart enabled a TCP listener: %v", after.ListenAddr)
	}
	if err := sandbox.waitForRunning(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonRestartFailsWhenRecordedListenAddressCannotStart(t *testing.T) {
	sandbox := newDaemonRestartSandbox(t)
	initialAddr, err := reserveLoopbackTCPAddr()
	if err != nil {
		t.Fatalf("reserve initial listen address: %v", err)
	}
	if output, err := sandbox.run("master", "start", "--listen", "tcp://"+initialAddr); err != nil {
		t.Fatalf("start master: %v\n%s", err, output)
	}
	if err := sandbox.waitForTCP(initialAddr, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create blocked listen address: %v", err)
	}
	defer blocker.Close()
	blockedAddr := "tcp://" + blocker.Addr().String()
	sandbox.overwriteRecordedListenAddr(t, blockedAddr)

	output, err := sandbox.run("daemon-restart")
	if err == nil {
		t.Fatalf("daemon-restart unexpectedly succeeded for blocked address %s:\n%s", blockedAddr, output)
	}
	if !strings.Contains(output, "daemon restart failed with recorded listen address") || !strings.Contains(output, blockedAddr) {
		t.Fatalf("daemon-restart failure did not identify the recorded listen address:\n%s", output)
	}
	statusOutput, statusErr := sandbox.run("daemon", "status")
	if statusErr != nil {
		t.Fatalf("daemon status after failed restart: %v\n%s", statusErr, statusOutput)
	}
	if !strings.Contains(statusOutput, "Status: not running") {
		t.Fatalf("failed restart left daemon reported as running:\n%s", statusOutput)
	}
	secondOutput, secondErr := sandbox.run("daemon-restart")
	if secondErr == nil || !strings.Contains(secondOutput, "no running daemon found") {
		t.Fatalf("daemon-restart without a running daemon did not fail clearly: err=%v output=%s", secondErr, secondOutput)
	}
}
