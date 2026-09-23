// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmdutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	internalclient "code.byted.org/lark_search/larksuite-cli/internal/client"
)

func TestNewSDKHTTPClientIncludesSingleAttemptTransport(t *testing.T) {
	sentinel := errors.New("production SDK transport sentinel")
	calls := 0
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: sentinel}
	})

	sdk := lark.NewClient(
		"test-app",
		"test-secret",
		lark.WithEnableTokenCache(false),
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithHttpClient(newSDKHTTPClient(base)),
		lark.WithOpenBaseUrl("https://open.example.test"),
	)
	_, err := sdk.Do(
		internalclient.WithSingleTransportAttempt(context.Background()),
		&larkcore.ApiReq{
			HttpMethod:                http.MethodGet,
			ApiPath:                   "/open-apis/test/v1/resource",
			SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeNone},
		},
	)
	if err == nil {
		t.Fatal("SDK request error = nil")
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1 from production SDK client wiring", calls)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("production SDK client wiring did not preserve the transport cause")
	}
}
