package daemon

import (
	"context"
	"time"
)

type ProcessManager interface {
	StartServerProcess(projectRoot string, port int) (*managedServer, error)
	IsServerProcessAlive(srv *managedServer) bool
	WaitForHealthy(srv *managedServer, timeout time.Duration, isHealthy func(context.Context) bool) error
	StopServerLocked(srv *managedServer)
}

type defaultProcessManager struct {
	server *SocketServer
}

func (p *defaultProcessManager) StartServerProcess(projectRoot string, port int) (*managedServer, error) {
	return p.server.startServerProcess(projectRoot, port)
}

func (p *defaultProcessManager) IsServerProcessAlive(srv *managedServer) bool {
	return p.server.isServerProcessAlive(srv)
}

func (p *defaultProcessManager) WaitForHealthy(srv *managedServer, timeout time.Duration, isHealthy func(context.Context) bool) error {
	return p.server.waitForOpenCodeServerHealthy(srv, timeout, isHealthy)
}

func (p *defaultProcessManager) StopServerLocked(srv *managedServer) {
	p.server.stopServerLocked(srv)
}
