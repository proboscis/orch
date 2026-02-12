package daemon

import (
	"context"
	"time"
)

// ProcessManager abstracts opencode server process lifecycle.
type ProcessManager interface {
	StartServerProcess(projectRoot string, port int) (*managedServer, error)
	StopServerLocked(srv *managedServer)
	WaitForHealthy(srv *managedServer, timeout time.Duration, isHealthy func(context.Context) bool) error
	IsServerProcessAlive(srv *managedServer) bool
}

type socketProcessManager struct {
	server *SocketServer
}

func newSocketProcessManager(server *SocketServer) ProcessManager {
	return &socketProcessManager{server: server}
}

func (p *socketProcessManager) StartServerProcess(projectRoot string, port int) (*managedServer, error) {
	return p.server.startServerProcess(projectRoot, port)
}

func (p *socketProcessManager) StopServerLocked(srv *managedServer) {
	p.server.stopServerLocked(srv)
}

func (p *socketProcessManager) WaitForHealthy(srv *managedServer, timeout time.Duration, isHealthy func(context.Context) bool) error {
	return p.server.waitForOpenCodeServerHealthy(srv, timeout, isHealthy)
}

func (p *socketProcessManager) IsServerProcessAlive(srv *managedServer) bool {
	return p.server.isServerProcessAlive(srv)
}
