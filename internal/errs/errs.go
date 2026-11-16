// Package errs defines the error kinds that cross layer boundaries. The API
// layer maps each kind to one HTTP status.
package errs

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalid      = errors.New("invalid request")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrUnavailable  = errors.New("unavailable") // for example, the machine is offline
)
