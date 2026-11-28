// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

// withInjectedRulesCancel gives a direct streamRuleArray call the capability
// WithNativeRulesWriteCancellation derives from the native writer. The stream
// itself only ever sees an opaque writer.
func withInjectedRulesCancel(parent context.Context, native *deadlineRecordingWriter) context.Context {
	return context.WithValue(parent, cancelRuleWriteKey{}, cancelRuleWriteFunc(func() error {
		return native.SetWriteDeadline(time.Unix(1, 0))
	}))
}

func rulesStreamSignal(t *testing.T, ch <-chan struct{}, what string) error {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		return errors.New("watchdog: " + what)
	}
}

func assertPartialRulesJSON(t *testing.T, body string) {
	t.Helper()
	var shouldFail []any
	if json.Unmarshal([]byte(body), &shouldFail) == nil {
		t.Fatalf("partial JSON unexpectedly complete: %s", body)
	}
	if strings.Contains(body, "about:blank") {
		t.Fatalf("post-commit failure appended problem+json: %s", body)
	}
}

func TestStreamErrorPreCommitClassification(t *testing.T) {
	t.Parallel()
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}

	t.Run("snapshot failure before any chunk returns problem+json", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		failing := func(context.Context, int, func([]libraryprocessing.StoredRule) error) error {
			return errors.New("snapshot query failed")
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, failing), dw, req) {
			t.Fatal("pre-commit failure aborted")
		}
		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		// The only write is the problem body itself; no stream chunk, no flush.
		if dw.writeCalls != 1 || dw.flushCalls != 0 {
			t.Fatalf("writes=%d flushes=%d, want only the problem body write", dw.writeCalls, dw.flushCalls)
		}
		dw.assertNativeCutoffPreserved(t, cutoff)
	})

	t.Run("cancellation with one buffered batch never arms the native writer", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		streamer := func(sCtx context.Context, size int, batch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(2))(sCtx, size, batch); err != nil {
				return err
			}
			cancel()
			return nil
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatal("pre-commit cancellation aborted")
		}
		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		dw.assertNativeCutoffPreserved(t, cutoff)
	})
}

func TestStreamErrorPostCommitWriteAndFlushClassification(t *testing.T) {
	t.Parallel()
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}
	boom := errors.New("socket broken")

	for _, tc := range []struct {
		name            string
		flush           bool
		failCall        int
		writes, flushes int
		wantText        string
	}{
		{"first write after WriteHeader fails", false, 1, 1, 0, "write library processing rules"},
		{"first intermediate flush fails", true, 1, 1, 1, "flush library processing rules"},
		{"final write fails after a committed chunk", false, 2, 2, 1, "write library processing rules"},
		{"final flush fails after a committed chunk", true, 2, 2, 2, "flush library processing rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			newWriter := func() *deadlineRecordingWriter {
				dw := newDeadlineRecordingWriter()
				fail := func(call int) error {
					if call == tc.failCall {
						return boom
					}
					return nil
				}
				if tc.flush {
					dw.flushErr = fail
				} else {
					dw.writeErr = fail
				}
				return dw
			}
			cutoff := time.Now().Add(time.Minute)

			// Direct classification: committed, wrapped with context, no cancellation.
			direct := newWriter()
			direct.currentDeadline = cutoff
			committed, err := streamRuleArray(t.Context(), t.Context(), direct, 2, fakeBatchStreamer(sampleStoredRules(3)))
			if !committed || !errors.Is(err, boom) || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("(committed=%v, err=%v), want (true, %v wrapped by %q)", committed, err, boom, tc.wantText)
			}
			direct.assertNativeCutoffPreserved(t, cutoff)

			// Handler classification: abort instead of a problem body on a 200 response.
			dw := newWriter()
			dw.currentDeadline = cutoff
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
			if !invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw, req) {
				t.Fatalf("expected ErrAbortHandler; body: %s", dw.Body.String())
			}
			if dw.writeCalls != tc.writes || dw.flushCalls != tc.flushes {
				t.Fatalf("writes=%d flushes=%d, want %d and %d", dw.writeCalls, dw.flushCalls, tc.writes, tc.flushes)
			}
			if !tc.flush || tc.failCall != 2 {
				assertPartialRulesJSON(t, dw.Body.String())
			}
			// A plain I/O error leaves the live request and the server's own deadline alone.
			dw.assertNativeCutoffPreserved(t, cutoff)
		})
	}
}

func TestStreamErrorNativeCancellationFailures(t *testing.T) {
	t.Parallel()

	t.Run("cancellation setter failure is joined with the context error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("native write cancellation failed")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		attempted := make(chan struct{})
		dw := newDeadlineRecordingWriter()
		dw.setDeadlineErr = func(int, time.Time) error {
			close(attempted)
			return boom
		}
		var blockErr error
		dw.flushErr = func(call int) error {
			if call != 1 {
				return nil
			}
			// Cancel the caller mid-flush; the flush returns once the setter was tried.
			cancel()
			blockErr = rulesStreamSignal(t, attempted, "cancellation setter was not called")
			return blockErr
		}
		committed, err := streamRuleArray(withInjectedRulesCancel(ctx, dw), ctx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if blockErr != nil {
			t.Fatal(blockErr)
		}
		if !committed || !errors.Is(err, context.Canceled) || !errors.Is(err, boom) ||
			!strings.Contains(err.Error(), "cancel library processing rules write") {
			t.Fatalf("(committed=%v, err=%v), want (true, %v joined with %v)", committed, err, context.Canceled, boom)
		}
		if dw.setCalls != 1 {
			t.Fatalf("setter calls = %d, want exactly 1", dw.setCalls)
		}
	})

	t.Run("cancellation setter failure aborts the handler stream", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		attempted := make(chan struct{})
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		dw.setDeadlineErr = func(int, time.Time) error {
			close(attempted)
			return errors.New("native write cancellation failed")
		}
		var blockErr error
		dw.flushErr = func(call int) error {
			if call != 1 {
				return nil
			}
			cancel()
			blockErr = rulesStreamSignal(t, attempted, "cancellation setter was not called")
			return blockErr
		}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		aborted := invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw, req)
		if blockErr != nil {
			t.Fatal(blockErr)
		}
		if !aborted {
			t.Fatalf("expected ErrAbortHandler; body: %s", dw.Body.String())
		}
		assertPartialRulesJSON(t, dw.Body.String())
		// One attempted setter call failed: no deadline took effect, cutoff intact.
		if dw.setCalls != 1 || len(dw.deadlines) != 0 || !dw.currentDeadline.Equal(cutoff) {
			t.Fatalf("setter calls=%d successful=%v current=%v, want one failed attempt and cutoff %v", dw.setCalls, dw.deadlines, dw.currentDeadline, cutoff)
		}
	})

	t.Run("blocked intermediate write keeps its own error after native expiry", func(t *testing.T) {
		t.Parallel()
		writeBoom := errors.New("intermediate write interrupted")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		interrupted := make(chan struct{})
		dw := newDeadlineRecordingWriter()
		dw.onSetDeadline = func(_ int, d time.Time) {
			if d.Equal(time.Unix(1, 0)) {
				close(interrupted)
			}
		}
		var blockErr error
		dw.writeErr = func(call int) error {
			if call != 1 {
				return nil
			}
			cancel()
			if blockErr = rulesStreamSignal(t, interrupted, "blocked write was not given native expiry"); blockErr != nil {
				return blockErr
			}
			return writeBoom
		}
		committed, err := streamRuleArray(withInjectedRulesCancel(ctx, dw), ctx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if blockErr != nil {
			t.Fatal(blockErr)
		}
		if !committed || !errors.Is(err, writeBoom) {
			t.Fatalf("(committed=%v, err=%v), want (true, %v)", committed, err, writeBoom)
		}
		if dw.writeCalls != 1 || dw.flushCalls != 0 {
			t.Fatalf("writes=%d flushes=%d, want the blocked write to be the last I/O", dw.writeCalls, dw.flushCalls)
		}
		dw.assertNativeExpiredOnce(t)
	})

	t.Run("final phase ignores an earlier snapshot expiry and never arms the writer", func(t *testing.T) {
		t.Parallel()
		var snap *expiringSnapshotCtx
		withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
			var release context.CancelFunc
			snap, release = newExpiringSnapshotCtx(parent, time.Now().Add(5*time.Second))
			return snap, release
		}
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		writeBoom := errors.New("final write failed")
		dw.writeErr = func(call int) error {
			if call == 2 {
				return writeBoom
			}
			return nil
		}
		streamer := func(sCtx context.Context, size int, batch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(sCtx, size, batch); err != nil {
				return err
			}
			snap.expire()
			return nil
		}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), dw, req) {
			t.Fatalf("expected ErrAbortHandler on final write failure; body: %s", dw.Body.String())
		}
		if dw.writeCalls != 2 {
			t.Fatalf("writes = %d, want the final write attempted after snapshot expiry", dw.writeCalls)
		}
		dw.assertNativeCutoffPreserved(t, cutoff)
		assertPartialRulesJSON(t, dw.Body.String())
	})
}
