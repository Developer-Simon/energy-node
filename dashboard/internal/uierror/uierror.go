// Package uierror carries a catalog key and its parameters through an error
// chain, so the API can hand the dashboard a translatable message while
// Error() keeps the original text for logs and older clients.
package uierror

import "errors"

// Error is a validation or domain error with a catalog key. Text is what
// Error() returns and stays byte-identical to the message the code produced
// before localization.
type Error struct {
	Key    string
	Params map[string]any
	Text   string
}

func New(key, text string, params map[string]any) *Error {
	return &Error{Key: key, Params: params, Text: text}
}

func (e *Error) Error() string { return e.Text }

// From returns the first *Error in err's chain.
func From(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
