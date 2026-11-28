// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// flushErrTestWriter has both Flush and FlushError; FlushError must win.
type flushErrTestWriter struct {
	http.ResponseWriter
	err         error
	flushErrN   int
	legacyFlush int
}

func (w *flushErrTestWriter) FlushError() error { w.flushErrN++; return w.err }
func (w *flushErrTestWriter) Flush()            { w.legacyFlush++ }

func TestRuleArrayStreamWriterFlushPending(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("flush boom")
	rec := httptest.NewRecorder()
	plain := struct{ http.ResponseWriter }{rec}
	good := &flushErrTestWriter{ResponseWriter: httptest.NewRecorder()}
	bad := &flushErrTestWriter{ResponseWriter: httptest.NewRecorder(), err: sentinel}

	tests := []struct {
		name      string
		w         http.ResponseWriter
		wantErr   error
		wantFlush int
	}{
		{"normal flush", good, nil, 1},
		{"unsupported writer", plain, nil, 0},
		{"concrete FlushError propagates", bad, sentinel, 1},
	}
	t.Cleanup(func() {
		if got := rec.Body.String(); got != "[" {
			t.Fatalf("unsupported writer body = %q, want %q", got, "[")
		}
	})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sw := &ruleArrayStreamWriter{w: tc.w}
			err := sw.flushPending(t.Context())
			if tc.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("error identity lost: got %v, want %v", err, tc.wantErr)
			}
			if !sw.committed {
				t.Fatal("headers should be committed before flush")
			}
			if fw, ok := tc.w.(*flushErrTestWriter); ok {
				if fw.flushErrN != tc.wantFlush {
					t.Fatalf("FlushError calls = %d, want %d", fw.flushErrN, tc.wantFlush)
				}
				if fw.legacyFlush != 0 {
					t.Fatalf("Flush() called %d times; FlushError must take precedence", fw.legacyFlush)
				}
			}
		})
	}
}

func TestDecodeRuleListJSONRejectsNull(t *testing.T) {
	t.Parallel()
	if _, err := decodeRuleListJSON([]byte("null")); err == nil {
		t.Fatal("decodeRuleListJSON(null) succeeded, want error for nil slice")
	}
	empty, err := decodeRuleListJSON([]byte("[]"))
	if err != nil {
		t.Fatalf("decodeRuleListJSON([]): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("decodeRuleListJSON([]) = %#v, want non-nil empty slice", empty)
	}
}
