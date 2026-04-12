package orchapi

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrInvalidRef       = errors.New("invalid run reference")
	ErrAmbiguousRef     = errors.New("ambiguous run reference: multiple runs match")
	ErrAlreadyExists    = errors.New("already exists")
	ErrSessionNotFound  = errors.New("session not found")
	ErrDaemonNotRunning = errors.New("daemon not running")
	ErrTimeout          = errors.New("timeout")
)

type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	if e.ID != "" {
		return e.Resource + " not found: " + e.ID
	}
	return e.Resource + " not found"
}

func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

func IssueNotFound(id string) error {
	return &NotFoundError{Resource: "issue", ID: id}
}

func RunNotFound(ref string) error {
	return &NotFoundError{Resource: "run", ID: ref}
}

func SessionNotFound(session string) error {
	return &NotFoundError{Resource: "session", ID: session}
}

type AmbiguousRefError struct {
	ShortID string
	Matches int
}

func (e *AmbiguousRefError) Error() string {
	return "ambiguous short ID: " + e.ShortID + " matches multiple runs"
}

func (e *AmbiguousRefError) Is(target error) bool {
	return target == ErrAmbiguousRef
}
