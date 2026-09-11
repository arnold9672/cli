// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"code.byted.org/lark_search/larksuite-cli/errs"
)

func TestParseGraphQuerySpec(t *testing.T) {
	const validDetailFormat = "markdown"

	tests := []struct {
		name      string
		start     string
		end       string
		want      graphQuerySpec
		wantParam string
	}{
		{
			name:  "valid range",
			start: "10",
			end:   "20",
			want: graphQuerySpec{
				DetailFormat: validDetailFormat,
				Windows: []graphTimeWindow{
					{Index: 1, StartTimeSec: 10, EndTimeSec: 20},
				},
			},
		},
		{name: "negative start", start: "-1", end: "20", wantParam: "--start-time-sec"},
		{name: "zero span", start: "20", end: "20", wantParam: "--end-time-sec"},
		{name: "reverse range", start: "20", end: "10", wantParam: "--end-time-sec"},
		{name: "exactly seven days", start: "10", end: "604810", want: graphQuerySpec{
			DetailFormat: validDetailFormat,
			Windows: []graphTimeWindow{
				{Index: 1, StartTimeSec: 10, EndTimeSec: 86410},
				{Index: 2, StartTimeSec: 86410, EndTimeSec: 172810},
				{Index: 3, StartTimeSec: 172810, EndTimeSec: 259210},
				{Index: 4, StartTimeSec: 259210, EndTimeSec: 345610},
				{Index: 5, StartTimeSec: 345610, EndTimeSec: 432010},
				{Index: 6, StartTimeSec: 432010, EndTimeSec: 518410},
				{Index: 7, StartTimeSec: 518410, EndTimeSec: 604810},
			},
		}},
		{name: "seven days plus one second", start: "10", end: "604811", wantParam: "--end-time-sec"},
		{name: "end time overflow", start: "10", end: "9223372036854775808", wantParam: "--end-time-sec"},
		{name: "malformed start", start: "nope", end: "20", wantParam: "--start-time-sec"},
		{name: "malformed end", start: "10", end: "nope", wantParam: "--end-time-sec"},
		{name: "blank start", start: "  ", end: "20", wantParam: "--start-time-sec"},
		{name: "blank end", start: "10", end: "  ", wantParam: "--end-time-sec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGraphQuerySpec(validDetailFormat, tt.start, tt.end)
			if tt.wantParam == "" {
				if err != nil {
					t.Fatalf("parseGraphQuerySpec() error = %v", err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("parseGraphQuerySpec() = %#v, want %#v", got, tt.want)
				}
				return
			}

			if err == nil {
				t.Fatal("parseGraphQuerySpec() error = nil, want validation error")
			}
			var validationErr *errs.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want *errs.ValidationError", err)
			}
			if validationErr.Param != tt.wantParam {
				t.Fatalf("validation param = %q, want %q", validationErr.Param, tt.wantParam)
			}
		})
	}
}

func TestParseGraphQuerySpecRejectsInvalidDetailFormat(t *testing.T) {
	for _, tc := range []struct {
		name         string
		detailFormat string
	}{
		{name: "unsupported value", detailFormat: "xml"},
		{name: "empty value", detailFormat: ""},
		{name: "whitespace value", detailFormat: "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseGraphQuerySpec(tc.detailFormat, "10", "20")
			var validation *errs.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v, want typed validation", err, err)
			}
			if validation.Subtype != errs.SubtypeInvalidArgument || validation.Param != "--detail-format" {
				t.Fatalf("validation = subtype %q param %q, want invalid_argument --detail-format", validation.Subtype, validation.Param)
			}
		})
	}
}

func TestParseGraphQuerySpecDoesNotEchoRejectedValues(t *testing.T) {
	tests := []struct {
		name          string
		start         string
		end           string
		wantParam     string
		rejectedValue string
	}{
		{
			name:          "malformed start",
			start:         "start-time-secret-x9",
			end:           "20",
			wantParam:     "--start-time-sec",
			rejectedValue: "start-time-secret-x9",
		},
		{
			name:          "negative start",
			start:         "-887766554433",
			end:           "20",
			wantParam:     "--start-time-sec",
			rejectedValue: "-887766554433",
		},
		{
			name:          "malformed end",
			start:         "10",
			end:           "end-time-secret-x9",
			wantParam:     "--end-time-sec",
			rejectedValue: "end-time-secret-x9",
		},
		{
			name:          "non-increasing end",
			start:         "424242424242",
			end:           "313131313131",
			wantParam:     "--end-time-sec",
			rejectedValue: "313131313131",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGraphQuerySpec("markdown", tt.start, tt.end)
			if err == nil {
				t.Fatal("parseGraphQuerySpec() error = nil, want validation error")
			}
			var validationErr *errs.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want *errs.ValidationError", err)
			}
			if validationErr.Param != tt.wantParam {
				t.Fatalf("validation param = %q, want %q", validationErr.Param, tt.wantParam)
			}
			for field, value := range map[string]string{
				"err.Error()": err.Error(),
				"Message":     validationErr.Message,
				"Hint":        validationErr.Hint,
			} {
				if strings.Contains(value, tt.rejectedValue) {
					t.Fatalf("%s contains rejected input %q: %q", field, tt.rejectedValue, value)
				}
			}
		})
	}
}

func TestSplitGraphQueryWindows(t *testing.T) {
	tests := []struct {
		name  string
		start int64
		end   int64
		want  []graphTimeWindow
	}{
		{
			name:  "partial day",
			start: 10,
			end:   20,
			want:  []graphTimeWindow{{Index: 1, StartTimeSec: 10, EndTimeSec: 20}},
		},
		{
			name:  "exact day",
			start: 10,
			end:   86410,
			want:  []graphTimeWindow{{Index: 1, StartTimeSec: 10, EndTimeSec: 86410}},
		},
		{
			name:  "day plus one second",
			start: 10,
			end:   86411,
			want: []graphTimeWindow{
				{Index: 1, StartTimeSec: 10, EndTimeSec: 86410},
				{Index: 2, StartTimeSec: 86410, EndTimeSec: 86411},
			},
		},
		{
			name:  "exact seven days",
			start: 10,
			end:   604810,
			want: []graphTimeWindow{
				{Index: 1, StartTimeSec: 10, EndTimeSec: 86410},
				{Index: 2, StartTimeSec: 86410, EndTimeSec: 172810},
				{Index: 3, StartTimeSec: 172810, EndTimeSec: 259210},
				{Index: 4, StartTimeSec: 259210, EndTimeSec: 345610},
				{Index: 5, StartTimeSec: 345610, EndTimeSec: 432010},
				{Index: 6, StartTimeSec: 432010, EndTimeSec: 518410},
				{Index: 7, StartTimeSec: 518410, EndTimeSec: 604810},
			},
		},
		{
			name:  "near max int64",
			start: math.MaxInt64 - 1,
			end:   math.MaxInt64,
			want:  []graphTimeWindow{{Index: 1, StartTimeSec: math.MaxInt64 - 1, EndTimeSec: math.MaxInt64}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitGraphQueryWindows(tt.start, tt.end)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitGraphQueryWindows() = %#v, want %#v", got, tt.want)
			}
			for i, window := range got {
				if window.Index != i+1 {
					t.Fatalf("window %d index = %d, want %d", i, window.Index, i+1)
				}
				if i > 0 && got[i-1].EndTimeSec != window.StartTimeSec {
					t.Fatalf("window %d starts at %d, want adjacent boundary %d", i+1, window.StartTimeSec, got[i-1].EndTimeSec)
				}
				span := window.EndTimeSec - window.StartTimeSec
				if span < 1 || span > graphQueryWindowSec {
					t.Fatalf("window %d span = %d, want [1, %d]", i+1, span, graphQueryWindowSec)
				}
			}
		})
	}
}
