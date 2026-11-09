// Package githost keeps bare Git repositories in one folder and answers
// questions about them by running the system git binary, always with a
// fixed argument list, a time limit and an output limit. It knows nothing
// about sign-in or the audit log: the gateway does that.
package githost

import "fmt"

// Kind says what went wrong, so the gateway can pick an HTTP status.
type Kind int

const (
	Invalid  Kind = iota + 1 // the request doesn't make sense
	NotFound                 // no such repository, ref or path
	Exists                   // the name is taken, or nothing to do
	Busy                     // another change is running on the repository
	Timeout                  // git took too long
	Failed                   // git itself failed; details are in the log
)

// Error is a failure with a message fit to show a person.
type Error struct {
	Kind Kind
	Msg  string
	// stderr is git's own message, for callers that show part of it
	// (import). Never shown whole: it can name paths and settings.
	stderr string
}

func (e *Error) Error() string { return e.Msg }

func errf(k Kind, format string, a ...any) *Error {
	return &Error{Kind: k, Msg: fmt.Sprintf(format, a...)}
}
