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
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

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
