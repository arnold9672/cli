// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net"
	"net/http"
)

type singleTransportAttemptContextKey struct{}

// WithSingleTransportAttempt marks an SDK request so a dial failure is returned
// after the first transport attempt instead of triggering the SDK's implicit
// second attempt. The marker is request-scoped and has no effect on unmarked
// calls.
func WithSingleTransportAttempt(ctx context.Context) context.Context {
	return context.WithValue(ctx, singleTransportAttemptContextKey{}, true)
}

// NewSingleAttemptTransport wraps the SDK transport at its HTTP-client
// boundary. Unmarked requests preserve the underlying response and error
// unchanged. Marked errors gain private compatibility wrappers so
// oapi-sdk-go v3.5.4's direct *url.Error / *net.OpError assertion cannot
// classify a dial failure as retryable or discard a timeout cause, while
// errors.Is and errors.As can still traverse the cause chain.
func NewSingleAttemptTransport(base http.RoundTripper) http.RoundTripper {
	return &singleAttemptTransport{base: base}
}

type singleAttemptTransport struct {
	base http.RoundTripper
}

func (t *singleAttemptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil || !singleTransportAttempt(req.Context()) {
		return resp, err
	}
	return resp, &singleAttemptSDKBypassError{
		cause: &singleAttemptError{cause: err},
	}
}

func singleTransportAttempt(ctx context.Context) bool {
	marked, _ := ctx.Value(singleTransportAttemptContextKey{}).(bool)
	return marked
}

type singleAttemptError struct {
	cause error
}

// singleAttemptSDKBypassError intentionally does not implement net.Error. The
// SDK therefore returns it without converting timeout or dial failures into
// SDK errors that discard the original cause. WrapDoAPIError classifies the
// nested singleAttemptError after the SDK boundary.
type singleAttemptSDKBypassError struct {
	cause error
}

func (e *singleAttemptSDKBypassError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *singleAttemptSDKBypassError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *singleAttemptError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *singleAttemptError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *singleAttemptError) Timeout() bool {
	if e == nil {
		return false
	}
	var netErr net.Error
	return errors.As(e.cause, &netErr) && netErr.Timeout()
}

func (e *singleAttemptError) Temporary() bool {
	if e == nil {
		return false
	}
	var netErr net.Error
	return errors.As(e.cause, &netErr) && netErr.Temporary()
}
