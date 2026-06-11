package app

import "errors"

var (
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrBadRequest     = errors.New("bad request")
	ErrEmailSend      = errors.New("email send failed")
	ErrUpstreamFailed = errors.New("upstream failed")
)
