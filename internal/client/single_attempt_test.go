// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	"code.byted.org/lark_search/larksuite-cli/errs"
)

func TestSingleAttemptTransportControlsSDKDialRetry(t *testing.T) {
	t.Run("unmarked request keeps SDK retry behavior", func(t *testing.T) {
		transport := &countingDialFailureTransport{cause: errors.New("unmarked dial")}
		err := doSDKDialRequest(context.Background(), transport)
		if err == nil {
			t.Fatal("SDK request error = nil")
		}
		if transport.calls != 2 {
			t.Fatalf("RoundTrip calls = %d, want existing SDK behavior of 2", transport.calls)
		}
	})

	t.Run("marked request makes one attempt and preserves cause", func(t *testing.T) {
		sentinel := errors.New("marked dial sentinel")
		transport := &countingDialFailureTransport{cause: sentinel}
		err := doSDKDialRequest(WithSingleTransportAttempt(context.Background()), transport)
		if err == nil {
			t.Fatal("SDK request error = nil")
		}
		if transport.calls != 1 {
			t.Fatalf("RoundTrip calls = %d, want 1", transport.calls)
		}
		if !errors.Is(err, sentinel) {
			t.Fatal("marked SDK error did not preserve the transport cause chain")
		}
		var opErr *net.OpError
		if !errors.As(err, &opErr) {
			t.Fatalf("marked SDK error = %T %v, want *net.OpError in chain", err, err)
		}
	})
}

func TestSingleAttemptTransportPreservesMarkedTimeoutClassification(t *testing.T) {
	timeoutCause := &singleAttemptTimeoutError{message: "marked timeout sentinel"}
	transport := &countingDialFailureTransport{cause: timeoutCause}

	err := WrapDoAPIError(doSDKDialRequest(WithSingleTransportAttempt(context.Background()), transport))
	var networkErr *errs.NetworkError
	if !errors.As(err, &networkErr) {
		t.Fatalf("error = %T %v, want *errs.NetworkError", err, err)
	}
	if networkErr.Subtype != errs.SubtypeNetworkTimeout {
		t.Fatalf("subtype = %q, want %q", networkErr.Subtype, errs.SubtypeNetworkTimeout)
	}
	if transport.calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", transport.calls)
	}
	if !errors.Is(err, timeoutCause) {
		t.Fatal("marked timeout did not preserve the original cause")
	}
	var preservedCause *singleAttemptTimeoutError
	if !errors.As(err, &preservedCause) || preservedCause != timeoutCause {
		t.Fatalf("preserved timeout cause = %p, want %p", preservedCause, timeoutCause)
	}
}

func TestSingleAttemptErrorNilSafeNetError(t *testing.T) {
	var err *singleAttemptError
	if err.Error() != "" {
		t.Fatalf("nil Error() = %q, want empty", err.Error())
	}
	if err.Unwrap() != nil {
		t.Fatalf("nil Unwrap() = %v, want nil", err.Unwrap())
	}
	if err.Timeout() {
		t.Fatal("nil Timeout() = true, want false")
	}
	if err.Temporary() {
		t.Fatal("nil Temporary() = true, want false")
	}
	var _ net.Error = err
}

func TestSingleAttemptErrorDelegatesWrappedNetError(t *testing.T) {
	timeoutCause := &singleAttemptTimeoutError{message: "wrapped timeout sentinel"}
	err := &singleAttemptError{cause: fmt.Errorf("transport wrapper: %w", timeoutCause)}

	if !err.Timeout() {
		t.Fatal("Timeout() = false, want wrapped net.Error timeout classification")
	}
	if !err.Temporary() {
		t.Fatal("Temporary() = false, want wrapped net.Error temporary classification")
	}
}

func doSDKDialRequest(ctx context.Context, base http.RoundTripper) error {
	sdk := lark.NewClient(
		"test-app",
		"test-secret",
		lark.WithEnableTokenCache(false),
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithHttpClient(&http.Client{Transport: NewSingleAttemptTransport(base)}),
		lark.WithOpenBaseUrl("https://open.example.test"),
	)
	_, err := sdk.Do(ctx, &larkcore.ApiReq{
		HttpMethod:                http.MethodGet,
		ApiPath:                   "/open-apis/test/v1/resource",
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeNone},
	})
	return err
}

type countingDialFailureTransport struct {
	calls int
	cause error
}

type singleAttemptTimeoutError struct {
	message string
}

func (e *singleAttemptTimeoutError) Error() string   { return e.message }
func (e *singleAttemptTimeoutError) Timeout() bool   { return true }
func (e *singleAttemptTimeoutError) Temporary() bool { return true }

func (t *countingDialFailureTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	t.calls++
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: t.cause}
}
