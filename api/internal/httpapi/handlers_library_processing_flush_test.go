// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
	"github.com/lusoris/SnatchArr/api/internal/sso"
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

type deadlineRecordingWriter struct {
	*httptest.ResponseRecorder
	currentDeadline time.Time
	deadlines       []time.Time
	writeDeadlines  []time.Time
	flushDeadlines  []time.Time
	setCalls        int
	writeCalls      int
	flushCalls      int
	setDeadlineErr  func(call int, d time.Time) error
	onSetDeadline   func(call int, d time.Time)
	writeErr        func(call int) error
	flushErr        func(call int) error
}

func newDeadlineRecordingWriter() *deadlineRecordingWriter {
	return &deadlineRecordingWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (w *deadlineRecordingWriter) SetReadDeadline(_ time.Time) error {
	return nil
}

func (w *deadlineRecordingWriter) SetWriteDeadline(d time.Time) error {
	w.setCalls++
	if w.setDeadlineErr != nil {
		if err := w.setDeadlineErr(w.setCalls, d); err != nil {
			return err
		}
	}
	w.currentDeadline = d
	w.deadlines = append(w.deadlines, d)
	if w.onSetDeadline != nil {
		w.onSetDeadline(w.setCalls, d)
	}
	return nil
}

func (w *deadlineRecordingWriter) Write(p []byte) (int, error) {
	w.writeCalls++
	w.writeDeadlines = append(w.writeDeadlines, w.currentDeadline)
	if w.writeErr != nil {
		if err := w.writeErr(w.writeCalls); err != nil {
			return 0, err
		}
	}
	return w.ResponseRecorder.Write(p)
}

func (w *deadlineRecordingWriter) FlushError() error {
	w.flushCalls++
	w.flushDeadlines = append(w.flushDeadlines, w.currentDeadline)
	if w.flushErr != nil {
		return w.flushErr(w.flushCalls)
	}
	return nil
}

// assertStreamDeadlines verifies that each intermediate flush inside SnapshotReadTx armed a
// write deadline in [snapLo, snapHi] and restored the response deadline in [responseLo, responseHi]
// (or cleared to zero when responseLo is zero) before returning, while the post-commit finish
// write/flush armed [responseLo, responseHi] (or left the deadline cleared when responseLo is zero)
// and kept that response deadline in effect afterwards.
func (w *deadlineRecordingWriter) assertStreamDeadlines(
	t *testing.T,
	snapLo, snapHi time.Time,
	responseLo, responseHi time.Time,
	intermediateFlushes int,
) {
	t.Helper()
	wantOps := intermediateFlushes + 1
	if len(w.writeDeadlines) != wantOps {
		t.Fatalf("Write count = %d, want %d (deadlines=%v)", len(w.writeDeadlines), wantOps, w.writeDeadlines)
	}
	if len(w.flushDeadlines) != wantOps {
		t.Fatalf("FlushError count = %d, want %d (deadlines=%v)", len(w.flushDeadlines), wantOps, w.flushDeadlines)
	}

	if responseLo.IsZero() {
		if !responseHi.IsZero() {
			t.Fatalf("responseHi = %v must be zero when responseLo is zero", responseHi)
		}
		w.assertUnboundedResponseDeadlines(t, snapLo, snapHi, intermediateFlushes)
		return
	}
	if intermediateFlushes > 0 && snapLo.Equal(responseLo) && snapHi.Equal(responseHi) {
		w.assertSharedTightDeadlines(t, responseLo, responseHi, intermediateFlushes)
		return
	}
	if intermediateFlushes > 0 && !snapHi.Before(responseLo) {
		t.Fatalf("expected snapshot window [%v, %v] strictly before response window [%v, %v]", snapLo, snapHi, responseLo, responseHi)
	}
	w.assertRestoredResponseDeadlines(t, snapLo, snapHi, responseLo, responseHi, intermediateFlushes)
}

func (w *deadlineRecordingWriter) assertUnboundedResponseDeadlines(
	t *testing.T,
	snapLo, snapHi time.Time,
	intermediateFlushes int,
) {
	t.Helper()
	wantDeadlineCalls := 2 * intermediateFlushes
	if len(w.deadlines) != wantDeadlineCalls {
		t.Fatalf("SetWriteDeadline calls = %v (len %d), want len %d", w.deadlines, len(w.deadlines), wantDeadlineCalls)
	}
	for i := range intermediateFlushes {
		armed := w.deadlines[2*i]
		cleared := w.deadlines[2*i+1]
		if armed.IsZero() || armed.Before(snapLo) || armed.After(snapHi) {
			t.Fatalf("intermediate[%d] armed deadline %v outside [%v, %v]", i, armed, snapLo, snapHi)
		}
		if !cleared.IsZero() {
			t.Fatalf("intermediate[%d] clear deadline = %v, want zero before next tx step/commit", i, cleared)
		}
		if !w.writeDeadlines[i].Equal(armed) {
			t.Fatalf("Write[%d] active deadline = %v, want %v", i, w.writeDeadlines[i], armed)
		}
		if !w.flushDeadlines[i].Equal(armed) {
			t.Fatalf("FlushError[%d] active deadline = %v, want %v", i, w.flushDeadlines[i], armed)
		}
	}
	if !w.writeDeadlines[intermediateFlushes].IsZero() {
		t.Fatalf("finish Write active deadline = %v, want zero", w.writeDeadlines[intermediateFlushes])
	}
	if !w.flushDeadlines[intermediateFlushes].IsZero() {
		t.Fatalf("finish FlushError active deadline = %v, want zero", w.flushDeadlines[intermediateFlushes])
	}
	if !w.currentDeadline.IsZero() {
		t.Fatalf("final currentDeadline = %v, want zero", w.currentDeadline)
	}
}

func (w *deadlineRecordingWriter) assertSharedTightDeadlines(
	t *testing.T,
	responseLo, responseHi time.Time,
	intermediateFlushes int,
) {
	t.Helper()
	wantDeadlineCalls := intermediateFlushes + 1
	if len(w.deadlines) != wantDeadlineCalls {
		t.Fatalf("SetWriteDeadline calls = %v (len %d), want len %d", w.deadlines, len(w.deadlines), wantDeadlineCalls)
	}
	finArmed := w.deadlines[intermediateFlushes]
	if finArmed.IsZero() || finArmed.Before(responseLo) || finArmed.After(responseHi) {
		t.Fatalf("shared armed deadline %v outside [%v, %v]", finArmed, responseLo, responseHi)
	}
	for i := range wantDeadlineCalls {
		if !w.deadlines[i].Equal(finArmed) {
			t.Fatalf("SetWriteDeadline[%d] = %v, want %v", i, w.deadlines[i], finArmed)
		}
		if !w.writeDeadlines[i].Equal(finArmed) {
			t.Fatalf("Write[%d] active deadline = %v, want %v", i, w.writeDeadlines[i], finArmed)
		}
		if !w.flushDeadlines[i].Equal(finArmed) {
			t.Fatalf("FlushError[%d] active deadline = %v, want %v", i, w.flushDeadlines[i], finArmed)
		}
	}
	if !w.currentDeadline.Equal(finArmed) {
		t.Fatalf("final currentDeadline = %v, want %v", w.currentDeadline, finArmed)
	}
}

func (w *deadlineRecordingWriter) assertRestoredResponseDeadlines(
	t *testing.T,
	snapLo, snapHi time.Time,
	responseLo, responseHi time.Time,
	intermediateFlushes int,
) {
	t.Helper()
	wantDeadlineCalls := 2*intermediateFlushes + 1
	if len(w.deadlines) != wantDeadlineCalls {
		t.Fatalf("SetWriteDeadline calls = %v (len %d), want len %d", w.deadlines, len(w.deadlines), wantDeadlineCalls)
	}
	finArmed := w.deadlines[2*intermediateFlushes]
	if finArmed.IsZero() || finArmed.Before(responseLo) || finArmed.After(responseHi) {
		t.Fatalf("finish armed deadline %v outside [%v, %v]", finArmed, responseLo, responseHi)
	}
	for i := range intermediateFlushes {
		armed := w.deadlines[2*i]
		restored := w.deadlines[2*i+1]
		if armed.IsZero() || armed.Before(snapLo) || armed.After(snapHi) {
			t.Fatalf("intermediate[%d] armed deadline %v outside [%v, %v]", i, armed, snapLo, snapHi)
		}
		if !restored.Equal(finArmed) {
			t.Fatalf("intermediate[%d] restored deadline = %v, want response deadline %v", i, restored, finArmed)
		}
		if !w.writeDeadlines[i].Equal(armed) {
			t.Fatalf("Write[%d] active deadline = %v, want %v", i, w.writeDeadlines[i], armed)
		}
		if !w.flushDeadlines[i].Equal(armed) {
			t.Fatalf("FlushError[%d] active deadline = %v, want %v", i, w.flushDeadlines[i], armed)
		}
	}
	if !w.writeDeadlines[intermediateFlushes].Equal(finArmed) {
		t.Fatalf("finish Write active deadline = %v, want %v", w.writeDeadlines[intermediateFlushes], finArmed)
	}
	if !w.flushDeadlines[intermediateFlushes].Equal(finArmed) {
		t.Fatalf("finish FlushError active deadline = %v, want %v", w.flushDeadlines[intermediateFlushes], finArmed)
	}
	if !w.currentDeadline.Equal(finArmed) {
		t.Fatalf("final currentDeadline = %v, want %v", w.currentDeadline, finArmed)
	}
}

type deadlineObservingChecker struct {
	enabled     bool
	err         error
	deadline    time.Time
	hasDeadline bool
}

func (c *deadlineObservingChecker) IsEnabled(ctx context.Context, _ string) (bool, error) {
	c.deadline, c.hasDeadline = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.err != nil {
		return false, c.err
	}
	return c.enabled, nil
}

// expiringSnapshotCtx wraps a parent request context with an explicit snapshot deadline
// that tests can deterministically expire right after SnapshotReadTx commits, without
// wall-clock sleeps or sub-second timers, while latching the first terminal error per
// context.Context's stability contract.
type expiringSnapshotCtx struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	stop     func() bool
	mu       sync.Mutex
	err      error
}

func newExpiringSnapshotCtx(parent context.Context, deadline time.Time) (*expiringSnapshotCtx, context.CancelFunc) {
	if pd, ok := parent.Deadline(); ok && pd.Before(deadline) {
		deadline = pd
	}
	c := &expiringSnapshotCtx{
		Context:  parent,
		deadline: deadline,
		done:     make(chan struct{}),
	}
	c.mu.Lock()
	if parentErr := parent.Err(); parentErr != nil {
		c.err = parentErr
		close(c.done)
		c.mu.Unlock()
		return c, func() {}
	}
	c.stop = context.AfterFunc(parent, func() {
		c.latchErr(parent.Err())
	})
	c.mu.Unlock()
	return c, func() {
		c.terminate(context.Canceled)
	}
}

func (c *expiringSnapshotCtx) latchErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil || err == nil {
		return
	}
	c.err = err
	close(c.done)
}

func (c *expiringSnapshotCtx) terminate(cause error) {
	c.mu.Lock()
	stop := c.stop
	if c.err == nil {
		if parentErr := c.Context.Err(); parentErr != nil {
			c.err = parentErr
		} else {
			c.err = cause
		}
		close(c.done)
	}
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (c *expiringSnapshotCtx) expire() {
	c.terminate(context.DeadlineExceeded)
}

func (c *expiringSnapshotCtx) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *expiringSnapshotCtx) Done() <-chan struct{} {
	return c.done
}

func (c *expiringSnapshotCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		if parentErr := c.Context.Err(); parentErr != nil {
			c.err = parentErr
			close(c.done)
		}
	}
	return c.err
}

func invokeCapturingAbort(h http.HandlerFunc, w http.ResponseWriter, req *http.Request) bool {
	var aborted bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				if rErr, ok := r.(error); ok && errors.Is(rErr, http.ErrAbortHandler) {
					aborted = true
					return
				}
				panic(r)
			}
		}()
		h(w, req)
	}()
	return aborted
}

func assertProblemResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var prob map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("decode problem: %v; body: %s", err, rec.Body.String())
	}
}

func TestServeLibraryProcessingRulesWriteDeadlinePreCommitAndErrors(t *testing.T) {
	t.Parallel()
	const snapshotContract = 5 * time.Second

	t.Run("pre-commit service failure bounds service by snapshot budget without arming writer", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: false}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		dw := newDeadlineRecordingWriter()
		before := time.Now()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.ServeLibraryProcessingRules()(dw, req)
		after := time.Now()

		assertProblemResponse(t, dw.ResponseRecorder, http.StatusNotFound)
		if !chk.hasDeadline || chk.deadline.Before(before.Add(snapshotContract)) || chk.deadline.After(after.Add(snapshotContract)) {
			t.Fatalf("service deadline = %v (has=%v), outside snapshot window", chk.deadline, chk.hasDeadline)
		}
		if len(dw.deadlines) != 0 || !dw.currentDeadline.IsZero() {
			t.Fatalf("writer deadlines = %v (current=%v), want none armed on pre-commit error", dw.deadlines, dw.currentDeadline)
		}
	})

	t.Run("shorter caller deadline propagates to snapshot and final write", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		callerDeadline, _ := ctx.Deadline()
		dw := newDeadlineRecordingWriter()
		var streamDeadline time.Time
		streamer := func(sCtx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			streamDeadline, _ = sCtx.Deadline()
			return fakeBatchStreamer(sampleStoredRules(3))(sCtx, batchSize, onBatch)
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, streamer)(dw, req)

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		if !streamDeadline.Equal(callerDeadline) {
			t.Fatalf("snapshot stream deadline = %v, want caller deadline %v", streamDeadline, callerDeadline)
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 3 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		dw.assertStreamDeadlines(t, callerDeadline, callerDeadline, callerDeadline, callerDeadline, 1)
	})

	t.Run("looser caller deadline caps intermediate snapshot writes at 5s and keeps caller deadline for finish", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()
		callerDeadline, _ := ctx.Deadline()
		dw := newDeadlineRecordingWriter()
		var streamDeadline time.Time
		streamer := func(sCtx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			streamDeadline, _ = sCtx.Deadline()
			return fakeBatchStreamer(sampleStoredRules(5))(sCtx, batchSize, onBatch)
		}
		before := time.Now()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, streamer)(dw, req)
		after := time.Now()

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		snapLo := before.Add(snapshotContract)
		snapHi := after.Add(snapshotContract)
		if streamDeadline.Before(snapLo) || streamDeadline.After(snapHi) {
			t.Fatalf("snapshot stream deadline = %v, want in [%v, %v]", streamDeadline, snapLo, snapHi)
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		dw.assertStreamDeadlines(t, snapLo, snapHi, callerDeadline, callerDeadline, 2)
	})

	t.Run("expired caller deadline returns problem+json without setting past write deadline", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
		defer cancel()
		dw := newDeadlineRecordingWriter()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.ServeLibraryProcessingRules()(dw, req)

		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		if len(dw.deadlines) != 0 || !dw.currentDeadline.IsZero() {
			t.Fatalf("deadlines = %v (current=%v), want none", dw.deadlines, dw.currentDeadline)
		}
	})
}

func TestListLibraryProcessingRulesPageMalformedRawQueryUnit(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
	srv, err := oas.NewServer(h, testNoopSecurity{},
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(slog.New(slog.DiscardHandler))),
	)
	if err != nil {
		t.Fatalf("oas.NewServer: %v", err)
	}
	handler := withHTTP(srv)

	malformed := []string{
		"limit=%zz",
		"cursor=%zz",
		"limit=1;cursor=abc",
		"limit=%zz&cursor=not-valid-base64!!!",
		"unknown=%zz",
	}
	for _, rq := range malformed {
		t.Run(rq, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules/page?"+rq, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			handler.ServeHTTP(rec, req)
			assertProblemResponse(t, rec, http.StatusBadRequest)
		})
	}
}

func TestLibraryProcessingRuleNilAndNullInputUnit(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
	adminCtx := WithUser(t.Context(), domain.User{ID: uuid.New(), Role: domain.RoleAdmin})
	ruleID := uuid.MustParse("018f0000-0000-7000-8000-000000000001")

	if _, err := libraryProcessingInputFromOAS(nil); !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("libraryProcessingInputFromOAS(nil) err = %v, want domain.ErrBadRequest", err)
	}
	if _, err := h.CreateLibraryProcessingRule(adminCtx, nil); !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("CreateLibraryProcessingRule(nil) err = %v, want domain.ErrBadRequest", err)
	}
	updateParams := oas.UpdateLibraryProcessingRuleParams{RuleId: ruleID}
	if _, err := h.UpdateLibraryProcessingRule(adminCtx, nil, updateParams); !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("UpdateLibraryProcessingRule(nil) err = %v, want domain.ErrBadRequest", err)
	}

	srv, err := oas.NewServer(h, testNoopSecurity{},
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(slog.New(slog.DiscardHandler))),
	)
	if err != nil {
		t.Fatalf("oas.NewServer: %v", err)
	}
	handler := withHTTP(srv)
	bodies := []string{
		"null",
		`{"scope":"global_default","rule":null}`,
		`{"scope":"global_default","rule":{"track":null}}`,
	}
	targets := []struct{ method, path string }{
		{http.MethodPost, PathPrefix + "/library-processing/rules"},
		{http.MethodPut, PathPrefix + "/library-processing/rules/" + ruleID.String()},
	}
	for _, body := range bodies {
		for _, target := range targets {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(adminCtx, target.method, target.path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(rec, req)
			assertProblemResponse(t, rec, http.StatusBadRequest)
		}
	}
}

func sampleStoredRules(n int) []libraryprocessing.StoredRule {
	baseTime := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	out := make([]libraryprocessing.StoredRule, 0, n)
	for i := range n {
		id := uuid.MustParse("018f0000-0000-7000-8000-000000000001")
		id[15] = byte(i + 1)
		out = append(out, libraryprocessing.StoredRule{
			ID:        id,
			Scope:     libraryprocessing.ScopeGlobalDefault,
			Rule:      libraryprocessing.Rule{Track: libraryprocessing.TrackRule{Languages: []string{"en"}}},
			CreatedAt: baseTime.Add(time.Duration(i) * time.Second),
			UpdatedAt: baseTime.Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

func fakeBatchStreamer(items []libraryprocessing.StoredRule) ruleStreamFunc {
	return func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for start := 0; start < len(items); start += batchSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+batchSize, len(items))
			if err := onBatch(items[start:end]); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
}

func TestServeLibraryProcessingRulesStreamWriterContractsUnit(t *testing.T) {
	t.Parallel()
	const snapshotContract = 5 * time.Second
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	t.Run("empty stream writes non-nil JSON array after commit without arming snapshot deadline", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(nil))(dw, req)

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || list == nil || len(list) != 0 {
			t.Fatalf("empty body %q -> (%#v, %v), want non-nil []", dw.Body.String(), list, err)
		}
		dw.assertStreamDeadlines(t, time.Time{}, time.Time{}, time.Time{}, time.Time{}, 0)
	})

	t.Run("multi-batch stream disarms snapshot deadline before commit and runs finish under request context", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(5)
		dw := newDeadlineRecordingWriter()
		var deadlineAtCommit time.Time
		seenAtCommit := false
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			// Simulate SnapshotReadTx.Commit: no snapshot write deadline may stay armed here.
			deadlineAtCommit = dw.currentDeadline
			seenAtCommit = true
			return nil
		}
		before := time.Now()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, streamer)(dw, req)
		after := time.Now()

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		if !seenAtCommit || !deadlineAtCommit.IsZero() {
			t.Fatalf("deadline armed during SnapshotReadTx commit = %v (seen=%v), want zero", deadlineAtCommit, seenAtCommit)
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		// 5 items with batchSize=2 -> 3 batches -> 2 intermediate flushPending inside tx + 1 finish after commit.
		dw.assertStreamDeadlines(t, before.Add(snapshotContract), after.Add(snapshotContract), time.Time{}, time.Time{}, 2)
	})

	t.Run("committed multi-batch stream finishes last batch when snapshot clock expires before finish", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(3)
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		callerDeadline, _ := reqCtx.Deadline()
		snapDeadline := time.Now().Add(snapshotContract)
		var expSnap *expiringSnapshotCtx
		withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
			var cancel context.CancelFunc
			expSnap, cancel = newExpiringSnapshotCtx(parent, snapDeadline)
			return expSnap, cancel
		}

		dw := newDeadlineRecordingWriter()
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			if !dw.currentDeadline.Equal(callerDeadline) {
				t.Errorf("write deadline armed at commit = %v, want caller response deadline %v", dw.currentDeadline, callerDeadline)
			}
			// SnapshotReadTx has committed; expire only the snapshot clock before finish writes batch 2.
			expSnap.expire()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesAt(2, withSnap, streamer)(dw, req)

		if !errors.Is(expSnap.Err(), context.DeadlineExceeded) {
			t.Fatalf("snapshotCtx.Err() = %v, want context.DeadlineExceeded", expSnap.Err())
		}
		if reqCtx.Err() != nil {
			t.Fatalf("reqCtx.Err() = %v, want nil (still-live caller)", reqCtx.Err())
		}
		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 3 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		dw.assertStreamDeadlines(t, snapDeadline, snapDeadline, callerDeadline, callerDeadline, 1)
	})

	t.Run("committed single-batch stream finishes when snapshot clock expires right after commit", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(2)
		snapDeadline := time.Now().Add(snapshotContract)
		var expSnap *expiringSnapshotCtx
		withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
			var cancel context.CancelFunc
			expSnap, cancel = newExpiringSnapshotCtx(parent, snapDeadline)
			return expSnap, cancel
		}

		dw := newDeadlineRecordingWriter()
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			expSnap.expire()
			return nil
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesAt(2, withSnap, streamer)(dw, req)

		if !errors.Is(expSnap.Err(), context.DeadlineExceeded) {
			t.Fatalf("snapshotCtx.Err() = %v, want context.DeadlineExceeded", expSnap.Err())
		}
		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 2 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		dw.assertStreamDeadlines(t, time.Time{}, time.Time{}, time.Time{}, time.Time{}, 0)
	})

	t.Run("request cancellation after commit before finish aborts post-header multi-batch stream", func(t *testing.T) {
		t.Parallel()
		reqCtx, reqCancel := context.WithCancel(t.Context())
		defer reqCancel()
		dw := newDeadlineRecordingWriter()
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			// SnapshotReadTx committed after flushing batch 1; caller disconnects before final write.
			reqCancel()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		aborted := invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, streamer), dw, req)
		if !aborted {
			t.Fatalf("expected ErrAbortHandler when caller cancels after commit; body: %s", dw.Body.String())
		}
		if !dw.currentDeadline.IsZero() {
			t.Fatalf("currentDeadline = %v, want cleared", dw.currentDeadline)
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("canceled multi-batch body parsed as complete JSON array: %s", dw.Body.String())
		}
	})

	t.Run("request cancellation after commit before finish returns problem+json on uncommitted single-batch stream", func(t *testing.T) {
		t.Parallel()
		reqCtx, reqCancel := context.WithCancel(t.Context())
		defer reqCancel()
		dw := newDeadlineRecordingWriter()
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(2))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			// Only 1 batch buffered in memory; headers have not been committed yet.
			reqCancel()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, streamer)(dw, req)
		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		if !dw.currentDeadline.IsZero() {
			t.Fatalf("currentDeadline = %v, want zero", dw.currentDeadline)
		}
	})

	t.Run("unsupported writer still streams valid JSON array", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		plain := struct{ http.ResponseWriter }{rec}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3)))(plain, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		list, err := decodeRuleListJSON(rec.Body.Bytes())
		if err != nil || len(list) != 3 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, rec.Body.String())
		}
	})
}

func TestStreamDeadlineClearWriteAndFlushErrorClassification(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	t.Run("set deadline error before headers returns problem+json", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		dw.setDeadlineErr = func(int, time.Time) error { return errors.New("conn deadline broken") }
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3)))(dw, req)

		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
	})

	t.Run("set deadline error after headers on second flush aborts stream", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		// Call 1 = batch 1 set, call 2 = batch 1 clear, call 3 = batch 2 set (after headers).
		dw.setDeadlineErr = func(call int, _ time.Time) error {
			if call == 3 {
				return errors.New("second batch deadline broken")
			}
			return nil
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(5))), dw, req) {
			t.Fatalf("expected ErrAbortHandler on post-header set deadline failure; body: %s", dw.Body.String())
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("partial JSON unexpectedly complete: %s", dw.Body.String())
		}
	})

	t.Run("clear deadline error before headers when canceled after arming returns problem+json", func(t *testing.T) {
		t.Parallel()
		reqCtx, reqCancel := context.WithCancel(t.Context())
		defer reqCancel()
		snapCtx, snapCancel := context.WithTimeout(reqCtx, 5*time.Second)
		defer snapCancel()
		clearBoom := errors.New("clear deadline broken pre-header")

		dw := newDeadlineRecordingWriter()
		dw.onSetDeadline = func(call int, d time.Time) {
			if call == 1 && !d.IsZero() {
				// Cancel before commitHeaders runs so intermediate writeChunk exits pre-header and clears to zero.
				reqCancel()
			}
		}
		dw.setDeadlineErr = func(_ int, d time.Time) error {
			if d.IsZero() {
				return clearBoom
			}
			return nil
		}

		// Direct check: streamRuleArray preserves both context.Canceled and clearBoom with committed=false.
		committed, directErr := streamRuleArray(reqCtx, snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if committed {
			t.Fatal("committed = true, want false before headers")
		}
		if !errors.Is(directErr, context.Canceled) || !errors.Is(directErr, clearBoom) {
			t.Fatalf("directErr = %v, want joined (%v, %v)", directErr, context.Canceled, clearBoom)
		}
		if !strings.Contains(directErr.Error(), "clear library processing rules write deadline") {
			t.Fatalf("directErr = %q, want clear deadline context", directErr.Error())
		}

		// Handler check: pre-header classification hits clearBoom and renders RFC 9457 problem+json.
		reqCtx2, reqCancel2 := context.WithCancel(t.Context())
		defer reqCancel2()
		dw2 := newDeadlineRecordingWriter()
		clearCalls := 0
		dw2.onSetDeadline = func(call int, d time.Time) {
			if call == 1 && !d.IsZero() {
				reqCancel2()
			}
		}
		dw2.setDeadlineErr = func(_ int, d time.Time) error {
			if d.IsZero() {
				clearCalls++
				return clearBoom
			}
			return nil
		}
		req := httptest.NewRequestWithContext(reqCtx2, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3)))(dw2, req)
		if clearCalls != 1 {
			t.Fatalf("clearCalls = %d, want 1", clearCalls)
		}
		assertProblemResponse(t, dw2.ResponseRecorder, http.StatusInternalServerError)
	})

	t.Run("clear deadline error after headers in intermediate flushPending aborts before commit", func(t *testing.T) {
		t.Parallel()
		clearBoom := errors.New("clear deadline broken on flushPending")
		dw := newDeadlineRecordingWriter()
		clearCalls := 0
		dw.setDeadlineErr = func(_ int, d time.Time) error {
			if d.IsZero() {
				clearCalls++
				return clearBoom
			}
			return nil
		}
		reachedCommit := false
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			reachedCommit = true
			return nil
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatalf("expected ErrAbortHandler on intermediate clear deadline failure; body: %s", dw.Body.String())
		}
		if reachedCommit || clearCalls != 1 {
			t.Fatalf("reachedCommit=%v clearCalls=%d, want (false, 1)", reachedCommit, clearCalls)
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("partial JSON unexpectedly complete: %s", dw.Body.String())
		}
	})

	t.Run("restore finite deadline error before headers when canceled after arming joins errors and returns problem+json", func(t *testing.T) {
		t.Parallel()
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		callerDeadline, _ := reqCtx.Deadline()
		snapCtx, snapCancel := context.WithTimeout(reqCtx, 5*time.Second)
		defer snapCancel()
		restoreBoom := errors.New("restore finite deadline broken pre-header")

		dw := newDeadlineRecordingWriter()
		dw.onSetDeadline = func(call int, d time.Time) {
			if call == 1 && !d.IsZero() {
				reqCancel()
			}
		}
		dw.setDeadlineErr = func(_ int, d time.Time) error {
			if d.Equal(callerDeadline) {
				return restoreBoom
			}
			return nil
		}

		committed, directErr := streamRuleArray(reqCtx, snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if committed {
			t.Fatal("committed = true, want false before headers")
		}
		if !errors.Is(directErr, context.Canceled) || !errors.Is(directErr, restoreBoom) {
			t.Fatalf("directErr = %v, want joined (%v, %v)", directErr, context.Canceled, restoreBoom)
		}
		if !strings.Contains(directErr.Error(), "restore library processing rules write deadline") {
			t.Fatalf("directErr = %q, want restore deadline context", directErr.Error())
		}

		reqCtx2, reqCancel2 := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel2()
		callerDeadline2, _ := reqCtx2.Deadline()
		dw2 := newDeadlineRecordingWriter()
		restoreCalls := 0
		dw2.onSetDeadline = func(call int, d time.Time) {
			if call == 1 && !d.IsZero() {
				reqCancel2()
			}
		}
		dw2.setDeadlineErr = func(_ int, d time.Time) error {
			if d.Equal(callerDeadline2) {
				restoreCalls++
				return restoreBoom
			}
			return nil
		}
		req := httptest.NewRequestWithContext(reqCtx2, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3)))(dw2, req)
		if restoreCalls != 1 {
			t.Fatalf("restoreCalls = %d, want 1", restoreCalls)
		}
		assertProblemResponse(t, dw2.ResponseRecorder, http.StatusInternalServerError)
	})

	t.Run("restore finite deadline error after headers in intermediate flushPending surfaces error and aborts before commit", func(t *testing.T) {
		t.Parallel()
		restoreBoom := errors.New("restore finite deadline broken on flushPending")
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		callerDeadline, _ := reqCtx.Deadline()
		snapCtx, snapCancel := context.WithTimeout(reqCtx, 5*time.Second)
		defer snapCancel()

		dw := newDeadlineRecordingWriter()
		dw.setDeadlineErr = func(_ int, d time.Time) error {
			if d.Equal(callerDeadline) {
				return restoreBoom
			}
			return nil
		}

		// Direct check: intermediate finite-deadline restoration error is returned with committed=true.
		committed, directErr := streamRuleArray(reqCtx, snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if !committed || !errors.Is(directErr, restoreBoom) {
			t.Fatalf("(committed=%v, err=%v), want (true, %v)", committed, directErr, restoreBoom)
		}
		if !strings.Contains(directErr.Error(), "restore library processing rules write deadline") {
			t.Fatalf("directErr = %q, want restore deadline context", directErr.Error())
		}

		// Handler check: post-header classification aborts before commit via ErrAbortHandler.
		dw2 := newDeadlineRecordingWriter()
		restoreCalls := 0
		dw2.setDeadlineErr = func(_ int, d time.Time) error {
			if d.Equal(callerDeadline) {
				restoreCalls++
				return restoreBoom
			}
			return nil
		}
		reachedCommit := false
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			reachedCommit = true
			return nil
		}
		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, streamer), dw2, req) {
			t.Fatalf("expected ErrAbortHandler when restoring finite caller deadline fails; body: %s", dw2.Body.String())
		}
		if reachedCommit || restoreCalls != 1 {
			t.Fatalf("reachedCommit=%v restoreCalls=%d, want (false, 1)", reachedCommit, restoreCalls)
		}
		var shouldFail []any
		if json.Unmarshal(dw2.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("partial JSON unexpectedly complete: %s", dw2.Body.String())
		}
	})

	t.Run("write error and finite deadline restore error in intermediate flushPending join both errors", func(t *testing.T) {
		t.Parallel()
		writeBoom := errors.New("intermediate write failed")
		restoreBoom := errors.New("intermediate finite restore failed")
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		callerDeadline, _ := reqCtx.Deadline()
		snapCtx, snapCancel := context.WithTimeout(reqCtx, 5*time.Second)
		defer snapCancel()

		dw := newDeadlineRecordingWriter()
		dw.writeErr = func(call int) error {
			if call == 1 {
				return writeBoom
			}
			return nil
		}
		dw.setDeadlineErr = func(_ int, d time.Time) error {
			if d.Equal(callerDeadline) {
				return restoreBoom
			}
			return nil
		}

		committed, directErr := streamRuleArray(reqCtx, snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if !committed || !errors.Is(directErr, writeBoom) || !errors.Is(directErr, restoreBoom) {
			t.Fatalf("(committed=%v, err=%v), want (true, joined %v + %v)", committed, directErr, writeBoom, restoreBoom)
		}
	})

	t.Run("set deadline error in finish after committed intermediate batch surfaces error and aborts stream", func(t *testing.T) {
		t.Parallel()
		finishSetBoom := errors.New("set deadline broken on finish")
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		snapCtx, snapCancel := context.WithTimeout(reqCtx, 5*time.Second)
		defer snapCancel()

		dw := newDeadlineRecordingWriter()
		dw.setDeadlineErr = func(call int, _ time.Time) error {
			if call == 3 {
				return finishSetBoom
			}
			return nil
		}

		committed, directErr := streamRuleArray(reqCtx, snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if !committed || !errors.Is(directErr, finishSetBoom) {
			t.Fatalf("(committed=%v, err=%v), want (true, %v)", committed, directErr, finishSetBoom)
		}

		dw2 := newDeadlineRecordingWriter()
		dw2.setDeadlineErr = func(call int, _ time.Time) error {
			if call == 3 {
				return finishSetBoom
			}
			return nil
		}
		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw2, req) {
			t.Fatalf("expected ErrAbortHandler when arming response deadline on finish fails")
		}
		var shouldFail []any
		if json.Unmarshal(dw2.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("partial JSON unexpectedly complete: %s", dw2.Body.String())
		}
	})

	t.Run("write error on first write after WriteHeader is classified post-header and clears deadline", func(t *testing.T) {
		t.Parallel()
		writeBoom := errors.New("socket broken on first write")
		dw := newDeadlineRecordingWriter()
		dw.writeErr = func(call int) error {
			if call == 1 {
				return writeBoom
			}
			return nil
		}

		// Direct check: committed=true and writeBoom preserved.
		snapCtx, snapCancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer snapCancel()
		committed, directErr := streamRuleArray(t.Context(), snapCtx, dw, 2, fakeBatchStreamer(sampleStoredRules(3)))
		if !committed || !errors.Is(directErr, writeBoom) {
			t.Fatalf("(committed=%v, err=%v), want (true, %v)", committed, directErr, writeBoom)
		}
		if !dw.currentDeadline.IsZero() {
			t.Fatalf("currentDeadline = %v, want cleared after write error", dw.currentDeadline)
		}

		// Handler check: aborts with ErrAbortHandler instead of writing superfluous problem+json onto 200 OK.
		dw2 := newDeadlineRecordingWriter()
		dw2.writeErr = func(call int) error {
			if call == 1 {
				return writeBoom
			}
			return nil
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw2, req) {
			t.Fatalf("expected ErrAbortHandler on first write failure; body: %s", dw2.Body.String())
		}
		if strings.Contains(dw2.Body.String(), "about:blank") {
			t.Fatalf("post-header write error appended problem+json: %s", dw2.Body.String())
		}
		if !dw2.currentDeadline.IsZero() {
			t.Fatalf("currentDeadline = %v, want cleared", dw2.currentDeadline)
		}
	})

	t.Run("write error on finish after committed intermediate batch aborts and keeps earlier caller deadline", func(t *testing.T) {
		t.Parallel()
		writeBoom := errors.New("socket broken on final write")
		baseReq := withServerWriteTimeout(
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			60*time.Second,
		)
		reqCtx, reqCancel := context.WithTimeout(baseReq.Context(), 20*time.Second)
		defer reqCancel()
		callerDeadline, _ := reqCtx.Deadline()
		dw := newDeadlineRecordingWriter()
		dw.writeErr = func(call int) error {
			if call == 2 {
				return writeBoom
			}
			return nil
		}
		req := baseReq.WithContext(reqCtx)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw, req) {
			t.Fatalf("expected ErrAbortHandler on final write failure; body: %s", dw.Body.String())
		}
		if !dw.currentDeadline.Equal(callerDeadline) {
			t.Fatalf("currentDeadline = %v, want preserved earlier caller deadline %v", dw.currentDeadline, callerDeadline)
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("truncated body parsed as complete JSON array: %s", dw.Body.String())
		}
	})

	t.Run("mid-stream flush failure aborts and cleans up write deadline", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		dw.flushErr = func(int) error { return errors.New("write deadline exceeded on flush") }
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))), dw, req) {
			t.Fatalf("expected ErrAbortHandler on flush failure; body: %s", dw.Body.String())
		}
		if !dw.currentDeadline.IsZero() {
			t.Fatalf("currentDeadline = %v, want cleared after abort", dw.currentDeadline)
		}
	})
}

func buildProductionMiddlewareStack(t *testing.T, logBuf *bytes.Buffer, rulesHandler http.HandlerFunc) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{
		libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
		logger:            logger,
	}
	srv, err := oas.NewServer(h, testNoopSecurity{},
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(logger)),
	)
	if err != nil {
		t.Fatalf("oas.NewServer: %v", err)
	}
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		sso.ProxyHeaderMiddleware(config.ProxyAuthOptions{}, logger, ErrorHandler(logger)),
		middleware.TrustProxy(middleware.TrustProxyOptions{}),
		middleware.Recover(logger),
		middleware.Logger(logger, clock.NewFake()),
		middleware.SecureHeaders(middleware.SecureHeadersDefaults()),
	)
	r.With(withHTTP, csrfUnlessBearer(CrossOrigin(logger))).Handle(PathPrefix+"/*", dispatchStreamingRoutes(srv, rulesHandler))
	return r
}

func TestServeLibraryProcessingRulesProductionMiddlewareAndNativeHTTP(t *testing.T) {
	t.Parallel()

	t.Run("real net/http server writer preserves deadline and flush support through middleware stack", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}

		var controllerCheckErr error
		rawStream := h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(5)))
		wrappedRules := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify httpsnoop.Wrap in Recover + Logger preserves ResponseController SetWriteDeadline & Flush.
			rc := http.NewResponseController(w)
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				controllerCheckErr = err
			} else if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				controllerCheckErr = err
			}
			rawStream(w, r)
		})

		router := buildProductionMiddlewareStack(t, &logBuf, wrappedRules)
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /rules: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Fatalf("read response body: %v", readErr)
		}
		if controllerCheckErr != nil {
			t.Fatalf("production ResponseWriter failed SetWriteDeadline through middleware: %v", controllerCheckErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, string(body))
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
		}
		list, decErr := decodeRuleListJSON(body)
		if decErr != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), decErr, string(body))
		}
	})

	t.Run("mid-stream abort re-panics ErrAbortHandler through Recover and closes native HTTP stream", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}

		boom := errors.New("second batch query failed")
		failingStream := h.serveLibraryProcessingRulesWith(2, func(_ context.Context, _ int, onBatch func([]libraryprocessing.StoredRule) error) error {
			rules := sampleStoredRules(4)
			if err := onBatch(rules[:2]); err != nil {
				return err
			}
			// Second onBatch flushes rules[:2] (committing 200 OK headers), then we fail inside tx.
			if err := onBatch(rules[2:]); err != nil {
				return err
			}
			return boom
		})

		router := buildProductionMiddlewareStack(t, &logBuf, failingStream)
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")

		resp, doErr := client.Do(req)
		if doErr == nil {
			defer func() { _ = resp.Body.Close() }()
			body, readErr := io.ReadAll(resp.Body)
			if readErr == nil {
				t.Fatalf("expected truncated chunked stream read error after ErrAbortHandler, got clean body: %s", string(body))
			}
			if strings.Contains(string(body), "about:blank") {
				t.Fatalf("Recover middleware wrote problem+json after committed headers: %s", string(body))
			}
		}
	})

	t.Run("unsupported writer wrapped by Recover and Logger degrades cleanly without false flush errors", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}
		router := buildProductionMiddlewareStack(t, &logBuf, h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))))

		rec := httptest.NewRecorder()
		plain := struct{ http.ResponseWriter }{rec}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		router.ServeHTTP(plain, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		list, err := decodeRuleListJSON(rec.Body.Bytes())
		if err != nil || len(list) != 3 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, rec.Body.String())
		}
		if strings.Contains(logBuf.String(), "httpx: flush response") {
			t.Fatalf("middleware logged unexpected flush error for unsupported writer:\n%s", logBuf.String())
		}
	})
}
