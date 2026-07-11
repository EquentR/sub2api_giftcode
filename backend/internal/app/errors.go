package app

import (
	"errors"
	"fmt"
)

var (
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrBadRequest     = errors.New("bad request")
	ErrEmailSend      = errors.New("email send failed")
	ErrUpstreamFailed = errors.New("upstream failed")
)

type TierConcurrencyConflictError struct {
	GroupID                int64
	ExistingConcurrency    int
	ConflictingConcurrency int
}

func (e *TierConcurrencyConflictError) Error() string {
	return fmt.Sprintf("subscription group %d has conflicting concurrency values %d and %d", e.GroupID, e.ExistingConcurrency, e.ConflictingConcurrency)
}

func (e *TierConcurrencyConflictError) Unwrap() error { return ErrBadRequest }
