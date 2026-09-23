// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"code.byted.org/lark_search/larksuite-cli/errs"
	extcs "code.byted.org/lark_search/larksuite-cli/extension/contentsafety"
	"code.byted.org/lark_search/larksuite-cli/internal/output"
)

type outStreamErrorWriter struct {
	err error
}

func (w outStreamErrorWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

type outStreamCancelingProvider struct {
	cancel context.CancelFunc
	scans  int
}

func (p *outStreamCancelingProvider) Name() string { return "out-stream-cancel-test" }

func (p *outStreamCancelingProvider) Scan(_ context.Context, _ extcs.ScanRequest) (*extcs.Alert, error) {
	p.scans++
	if p.cancel != nil {
		p.cancel()
	}
	return nil, nil
}

func registerOutStreamTestProvider(t *testing.T, provider extcs.Provider) {
	t.Helper()
	previous := extcs.GetProvider()
	extcs.Register(provider)
	t.Cleanup(func() { extcs.Register(previous) })
}

func TestOutStreamJSONEmitsCompactEnvelope(t *testing.T) {
	rctx, stdout, _ := newCSTestContext(t)
	rctx.Format = "json"

	err := rctx.OutStream(map[string]any{"window_index": 1}, nil)
	if err != nil {
		t.Fatalf("OutStream() error = %v", err)
	}

	if got, want := stdout.String(), `{"ok":true,"identity":"bot","data":{"window_index":1}}
`; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want one trailing newline", stdout.String())
	}

	var env output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK || env.Identity != "bot" {
		t.Fatalf("envelope = %#v, want ok=true and bot identity", env)
	}
}

func TestOutStreamCanceledBeforeScanDoesNotScanOrWrite(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "warn")
	provider := &outStreamCancelingProvider{}
	registerOutStreamTestProvider(t, provider)

	rctx, stdout, _ := newCSTestContext(t)
	rctx.Format = "json"
	ctx, cancel := context.WithCancel(rctx.ctx)
	cancel()
	rctx.ctx = ctx

	err := rctx.OutStream(map[string]any{"window_index": 1}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OutStream() error = %v, want context.Canceled", err)
	}
	if provider.scans != 0 {
		t.Fatalf("content-safety scans = %d, want 0", provider.scans)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestOutStreamCancellationDuringScanDoesNotWrite(t *testing.T) {
	for _, format := range []string{"json", "pretty"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "warn")
			rctx, stdout, _ := newCSTestContext(t)
			rctx.Format = format
			ctx, cancel := context.WithCancel(rctx.ctx)
			rctx.ctx = ctx
			provider := &outStreamCancelingProvider{cancel: cancel}
			registerOutStreamTestProvider(t, provider)

			rendered := false
			err := rctx.OutStream(map[string]any{"window_index": 1}, func(io.Writer) {
				rendered = true
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("OutStream() error = %v, want context.Canceled", err)
			}
			if provider.scans != 1 {
				t.Fatalf("content-safety scans = %d, want 1", provider.scans)
			}
			if rendered {
				t.Fatal("pretty renderer called after cancellation during scan")
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestOutStreamEncoderFailureReturnsTypedError(t *testing.T) {
	sentinelText := "distinctive streaming write failure"
	sentinel := errors.New(sentinelText)
	rctx, _, _ := newCSTestContext(t)
	rctx.Format = "json"
	rctx.IO().Out = outStreamErrorWriter{err: sentinel}

	err := rctx.OutStream(map[string]any{"window_index": 1}, nil)
	if err == nil {
		t.Fatal("OutStream() error = nil, want encoder failure")
	}
	var internalErr *errs.InternalError
	if !errors.As(err, &internalErr) {
		t.Fatalf("error = %T, want *errs.InternalError", err)
	}
	problem, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("errs.ProblemOf(%T) did not find a typed problem", err)
	}
	if problem.Category != errs.CategoryInternal || problem.Subtype != errs.SubtypeUnknown {
		t.Fatalf("problem = %s/%s, want %s/%s", problem.Category, problem.Subtype, errs.CategoryInternal, errs.SubtypeUnknown)
	}
	if strings.Contains(problem.Message, sentinelText) || strings.Contains(problem.Hint, sentinelText) {
		t.Fatalf("serialized problem leaks writer failure: message=%q hint=%q", problem.Message, problem.Hint)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("encoder error does not preserve the writer failure cause")
	}
}

func TestOutStreamJSONPreservesPendingNotice(t *testing.T) {
	previousNotice := output.PendingNotice
	output.PendingNotice = func() map[string]interface{} {
		return map[string]interface{}{"stream_review": "notice-preserved"}
	}
	t.Cleanup(func() { output.PendingNotice = previousNotice })

	rctx, stdout, _ := newCSTestContext(t)
	rctx.Format = "json"
	if err := rctx.OutStream(map[string]any{"window_index": 1}, nil); err != nil {
		t.Fatalf("OutStream() error = %v", err)
	}

	var env output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got, want := env.Notice["stream_review"], "notice-preserved"; got != want {
		t.Fatalf("_notice.stream_review = %v, want %q", got, want)
	}
}

func TestOutStreamPrettyCallsRendererWithoutJSON(t *testing.T) {
	rctx, stdout, _ := newCSTestContext(t)
	rctx.Format = "pretty"
	called := false

	err := rctx.OutStream(map[string]any{"window_index": 1}, func(w io.Writer) {
		called = true
		_, _ = fmt.Fprint(w, "pretty output\n")
	})
	if err != nil {
		t.Fatalf("OutStream() error = %v", err)
	}
	if !called {
		t.Fatal("pretty renderer was not called")
	}
	if got, want := stdout.String(), "pretty output\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestOutStreamWarnJSONEmbedsAlert(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "warn")
	registerOutStreamTestProvider(t, &csTestProvider{alert: &extcs.Alert{Provider: "test", MatchedRules: []string{"r1"}}})

	rctx, stdout, stderr := newCSTestContext(t)
	rctx.Format = "json"
	if err := rctx.OutStream(map[string]any{"msg": "hello"}, nil); err != nil {
		t.Fatalf("OutStream() error = %v", err)
	}

	var env output.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.ContentSafetyAlert == nil {
		t.Fatal("expected _content_safety_alert in envelope")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for JSON warning", stderr.String())
	}
}

func TestOutStreamWarnPrettyWritesAlertToStderr(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "warn")
	registerOutStreamTestProvider(t, &csTestProvider{alert: &extcs.Alert{Provider: "test", MatchedRules: []string{"r1"}}})

	rctx, stdout, stderr := newCSTestContext(t)
	rctx.Format = "pretty"
	if err := rctx.OutStream(map[string]any{"msg": "hello"}, func(w io.Writer) {
		_, _ = w.Write([]byte("pretty output\n"))
	}); err != nil {
		t.Fatalf("OutStream() error = %v", err)
	}
	if got, want := stdout.String(), "pretty output\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stderr.String(), "warning: content safety alert from test") {
		t.Fatalf("stderr = %q, want content-safety warning", stderr.String())
	}
}

func TestOutStreamBlockReturnsContentSafetyErrorWithoutStdout(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "block")
	registerOutStreamTestProvider(t, &csTestProvider{alert: &extcs.Alert{Provider: "test", MatchedRules: []string{"r1"}}})

	rctx, stdout, _ := newCSTestContext(t)
	rctx.Format = "json"
	err := rctx.OutStream(map[string]any{"msg": "hello"}, nil)
	if err == nil {
		t.Fatal("OutStream() error = nil, want content-safety error")
	}
	var safetyErr *errs.ContentSafetyError
	if !errors.As(err, &safetyErr) {
		t.Fatalf("error = %T, want *errs.ContentSafetyError", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}
