package daemon

// WorkerRPCRejectedError is returned by worker-plane client RPCs
// (RegisterWorker, WorkerHeartbeat, LeaseWork, AcknowledgeEffect,
// UnregisterWorker) when the master received the request and explicitly
// refused it (resp.ok == false), as opposed to a transport failure that
// never reached the master. The worker run loop relies on this distinction:
// transport failures are retried with backoff indefinitely, while a live
// master refusing registration (auth/policy) is fatal.
type WorkerRPCRejectedError struct {
	Message string
}

func (e *WorkerRPCRejectedError) Error() string {
	return "daemon error: " + e.Message
}

// ClientConfigError marks a client-side configuration problem (e.g. an
// unparseable daemon address) that no amount of reconnecting can fix.
type ClientConfigError struct {
	Message string
}

func (e *ClientConfigError) Error() string {
	return e.Message
}
