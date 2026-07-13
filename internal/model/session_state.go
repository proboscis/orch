package model

// SessionState is the stored-fact lifecycle view defined by ADR-0005 R6. It
// deliberately does not represent a liveness probe; Alive/AliveKnown remain
// the separate observation of the currently running multiplexer session.
type SessionState string

const (
	SessionStateLive              SessionState = "live"
	SessionStateReapedRevivable   SessionState = "reaped(revivable)"
	SessionStateReapedUnrevivable SessionState = "reaped(unrevivable)"
)
