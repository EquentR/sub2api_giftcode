package app

import (
	"errors"
	"fmt"
)

var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrBadRequest          = errors.New("bad request")
	ErrEmailSend           = errors.New("email send failed")
	ErrUpstreamFailed      = errors.New("upstream failed")
	ErrUpstreamUnavailable = errors.New("upstream unavailable")
)

type StableReasonError struct {
	Cause  error
	Reason string
}

func (e *StableReasonError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Reason
	}
	if e.Reason == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%s: %s", e.Cause, e.Reason)
}

func (e *StableReasonError) Unwrap() error { return e.Cause }

func StableReason(err error) string {
	var reasoned *StableReasonError
	if errors.As(err, &reasoned) {
		return reasoned.Reason
	}
	return ""
}

func withStableReason(cause error, reason string) error {
	return &StableReasonError{Cause: cause, Reason: reason}
}

type TierConcurrencyConflictError struct {
	GroupID                int64
	ExistingConcurrency    int
	ConflictingConcurrency int
}

func (e *TierConcurrencyConflictError) Error() string {
	return fmt.Sprintf("subscription group %d has conflicting concurrency values %d and %d", e.GroupID, e.ExistingConcurrency, e.ConflictingConcurrency)
}

func (e *TierConcurrencyConflictError) Unwrap() error { return ErrBadRequest }
