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

// deadlineRecordingWriter models the native writer. The production cancel
// setter runs on a context.AfterFunc goroutine while the handler goroutine is
// blocked in Write or Flush, so recorded state is guarded by mu. Hooks run
// outside mu so a blocked hook cannot stall the setter. Tests read the fields
// directly only after the handler returned: the handler joins the setter first.
type deadlineRecordingWriter struct {
	*httptest.ResponseRecorder
	mu              sync.Mutex
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
	w.mu.Lock()
	w.setCalls++
	call := w.setCalls
	w.mu.Unlock()
	if w.setDeadlineErr != nil {
		if err := w.setDeadlineErr(call, d); err != nil {
			return err
		}
	}
	w.mu.Lock()
	w.currentDeadline = d
	w.deadlines = append(w.deadlines, d)
	w.mu.Unlock()
	if w.onSetDeadline != nil {
		w.onSetDeadline(call, d)
	}
	return nil
}

func (w *deadlineRecordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writeCalls++
	call := w.writeCalls
	w.writeDeadlines = append(w.writeDeadlines, w.currentDeadline)
	w.mu.Unlock()
	if w.writeErr != nil {
		if err := w.writeErr(call); err != nil {
			return 0, err
		}
	}
	return w.ResponseRecorder.Write(p)
}

func (w *deadlineRecordingWriter) FlushError() error {
	w.mu.Lock()
	w.flushCalls++
	call := w.flushCalls
	w.flushDeadlines = append(w.flushDeadlines, w.currentDeadline)
	w.mu.Unlock()
	if w.flushErr != nil {
		return w.flushErr(call)
	}
	return nil
}

// assertNativeCutoffPreserved checks that no setter call ran and the native
// cutoff is unchanged.
func (w *deadlineRecordingWriter) assertNativeCutoffPreserved(t *testing.T, cutoff time.Time) {
	t.Helper()
	if w.setCalls != 0 || !w.currentDeadline.Equal(cutoff) {
		t.Fatalf("native cutoff %v became %v with %d setter calls: %v", cutoff, w.currentDeadline, w.setCalls, w.deadlines)
	}
}

// assertNativeExpiredOnce checks the single cancellation the handler may issue.
func (w *deadlineRecordingWriter) assertNativeExpiredOnce(t *testing.T) {
	t.Helper()
	if w.setCalls != 1 || !w.currentDeadline.Equal(time.Unix(1, 0)) {
		t.Fatalf("native expiry calls=%d current=%v deadlines=%v; want one expiry to the past", w.setCalls, w.currentDeadline, w.deadlines)
	}
}

func (w *deadlineRecordingWriter) assertNativeRulesCutoff(t *testing.T, cutoff time.Time, chunks int) {
	t.Helper()
	if w.setCalls != 0 || !w.currentDeadline.Equal(cutoff) {
		t.Fatalf("native cutoff %v became %v with %d setter calls: %v", cutoff, w.currentDeadline, w.setCalls, w.deadlines)
	}
	for _, observed := range [][]time.Time{w.writeDeadlines, w.flushDeadlines} {
		if len(observed) != chunks {
			t.Fatalf("write/flush cutoffs = %v, want %d observations", observed, chunks)
		}
		for _, d := range observed {
			if !d.Equal(cutoff) {
				t.Fatalf("cutoff during I/O = %v, want %v", d, cutoff)
			}
		}
	}
}

// Middleware forwards flushes but hides the native deadline setter.
type rulesDeadlineMiddlewareWriter struct{ http.ResponseWriter }

func (w rulesDeadlineMiddlewareWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func invokeRulesWithNativeCancellation(h http.HandlerFunc, w http.ResponseWriter, req *http.Request) bool {
	native := WithNativeRulesWriteCancellation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(rulesDeadlineMiddlewareWriter{w}, r)
	}))
	return invokeCapturingAbort(native.ServeHTTP, w, req)
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
	if prob == nil {
		t.Fatalf("expected a problem object; body: %s", rec.Body.String())
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
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		before := time.Now()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.ServeLibraryProcessingRules(), dw, req) {
			t.Fatal("pre-commit problem response aborted")
		}
		after := time.Now()

		assertProblemResponse(t, dw.ResponseRecorder, http.StatusNotFound)
		if !chk.hasDeadline || chk.deadline.Before(before.Add(snapshotContract)) || chk.deadline.After(after.Add(snapshotContract)) {
			t.Fatalf("service deadline = %v (has=%v), outside snapshot window", chk.deadline, chk.hasDeadline)
		}
		if dw.setCalls != 0 || !dw.currentDeadline.Equal(cutoff) {
			t.Fatalf("pre-commit error changed native cutoff: calls=%d current=%v", dw.setCalls, dw.currentDeadline)
		}
	})

	t.Run("short caller bounds snapshot without replacing native cutoff", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		callerDeadline, _ := ctx.Deadline()
		dw := newDeadlineRecordingWriter()
		cutoff := callerDeadline.Add(time.Minute)
		dw.currentDeadline = cutoff
		var streamDeadline time.Time
		streamer := func(sCtx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			streamDeadline, _ = sCtx.Deadline()
			return fakeBatchStreamer(sampleStoredRules(3))(sCtx, batchSize, onBatch)
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatal("successful stream aborted")
		}

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
		dw.assertNativeRulesCutoff(t, cutoff, 2)
	})

	t.Run("snapshot budget caps a looser caller without extending native cutoff", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(30 * time.Minute)
		dw.currentDeadline = cutoff
		var streamDeadline time.Time
		streamer := func(sCtx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			streamDeadline, _ = sCtx.Deadline()
			return fakeBatchStreamer(sampleStoredRules(5))(sCtx, batchSize, onBatch)
		}
		before := time.Now()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatal("successful stream aborted")
		}
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
		dw.assertNativeRulesCutoff(t, cutoff, 3)
	})

	t.Run("expired caller deadline returns problem+json without setting past write deadline", func(t *testing.T) {
		t.Parallel()
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
		defer cancel()
		dw := newDeadlineRecordingWriter()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.ServeLibraryProcessingRules(), dw, req) {
			t.Fatal("expired caller's pre-commit problem response aborted")
		}

		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		if dw.setCalls != 0 || !dw.currentDeadline.IsZero() {
			t.Fatalf("deadlines = %v (current=%v), want none", dw.deadlines, dw.currentDeadline)
		}
	})
}

func TestServeLibraryProcessingRulesBlockedIOCancellation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                        string
		final, flush, expireSnapshot, withoutNative bool
		writes, flushes                             int
	}{
		{name: "snapshot timeout interrupts flush", flush: true, expireSnapshot: true, writes: 1, flushes: 1},
		{name: "caller cancellation interrupts snapshot write", writes: 1},
		{name: "caller cancellation interrupts final write", final: true, writes: 2, flushes: 1},
		{name: "caller cancellation interrupts final flush", final: true, flush: true, writes: 2, flushes: 2},
		{name: "without native capability cancellation is checked after write", withoutNative: true, writes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dw := newDeadlineRecordingWriter()
			cutoff, expired := time.Now().Add(time.Minute), time.Unix(1, 0)
			dw.currentDeadline = cutoff
			interrupted := make(chan struct{})
			dw.onSetDeadline = func(call int, d time.Time) {
				if call == 1 && d.Equal(expired) {
					close(interrupted)
				}
			}
			var snapshot *expiringSnapshotCtx
			withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
				var release context.CancelFunc
				snapshot, release = newExpiringSnapshotCtx(parent, time.Now().Add(5*time.Second))
				return snapshot, release
			}
			var streamReturned, entered bool
			stream := func(sCtx context.Context, size int, batch func([]libraryprocessing.StoredRule) error) error {
				err := fakeBatchStreamer(sampleStoredRules(3))(sCtx, size, batch)
				streamReturned = err == nil
				return err
			}
			block := func(call int) error {
				wantCall := 1
				if tc.final {
					wantCall = 2
				}
				if call != wantCall {
					return nil
				}
				entered = true
				if streamReturned != tc.final {
					t.Errorf("stream returned successfully before I/O = %v, want %v", streamReturned, tc.final)
				}
				// Expire only the snapshot clock, or cancel the caller. Expiry is
				// injected, never waited for.
				if tc.expireSnapshot {
					snapshot.expire()
				} else {
					cancel()
				}
				if tc.withoutNative {
					return nil
				}
				timer := time.NewTimer(10 * time.Second)
				defer timer.Stop()
				select {
				case <-interrupted:
					return context.DeadlineExceeded
				case <-timer.C:
					t.Error("blocked I/O did not receive native expiry")
					return errors.New("test I/O watchdog expired")
				}
			}
			if tc.flush {
				dw.flushErr = block
			} else {
				dw.writeErr = block
			}
			h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}
			handler := h.serveLibraryProcessingRulesAt(2, withSnap, stream)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
			var aborted bool
			if tc.withoutNative {
				// A downstream setter alone must not confer native cancellation.
				aborted = invokeCapturingAbort(handler, dw, req)
			} else {
				aborted = invokeRulesWithNativeCancellation(handler, dw, req)
			}
			if !entered || !aborted || dw.Code != http.StatusOK || dw.writeCalls != tc.writes || dw.flushCalls != tc.flushes {
				t.Fatalf("entered=%v aborted=%v status=%d writes=%d flushes=%d; body: %s", entered, aborted, dw.Code, dw.writeCalls, dw.flushCalls, dw.Body.String())
			}
			if (!tc.final || !tc.flush) && json.Valid(dw.Body.Bytes()) {
				t.Fatalf("aborted stream emitted complete JSON: %s", dw.Body.String())
			}
			if tc.expireSnapshot && (!errors.Is(snapshot.Err(), context.DeadlineExceeded) || ctx.Err() != nil) {
				t.Fatalf("snapshot error=%v, caller error=%v; want only snapshot expiry", snapshot.Err(), ctx.Err())
			}
			if tc.withoutNative {
				dw.assertNativeCutoffPreserved(t, cutoff)
			} else {
				dw.assertNativeExpiredOnce(t)
			}
		})
	}
}

func TestServeLibraryProcessingRulesFinalWriteOutlivesSnapshot(t *testing.T) {
	t.Parallel()
	dw := newDeadlineRecordingWriter()
	cutoff := time.Now().Add(time.Minute)
	dw.currentDeadline = cutoff
	var snapshotDone <-chan struct{}
	var streamReturned bool
	stream := func(ctx context.Context, size int, batch func([]libraryprocessing.StoredRule) error) error {
		snapshotDone = ctx.Done()
		err := fakeBatchStreamer(sampleStoredRules(3))(ctx, size, batch)
		streamReturned = err == nil
		return err
	}
	dw.writeErr = func(call int) error {
		if call != 2 {
			return nil
		}
		if !streamReturned {
			return errors.New("final write started before the snapshot streamer returned")
		}
		// Hold the final write until the snapshot is released or its budget expires.
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-snapshotDone:
			return nil
		case <-timer.C:
			return errors.New("snapshot did not finish within its budget")
		}
	}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true})}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
	if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, stream), dw, req) {
		t.Fatalf("final write aborted after snapshot ended; body: %s", dw.Body.String())
	}
	list, err := decodeRuleListJSON(dw.Body.Bytes())
	if dw.Code != http.StatusOK || err != nil || len(list) != 3 {
		t.Fatalf("status=%d decoded len=%d err=%v; body: %s", dw.Code, len(list), err, dw.Body.String())
	}
	dw.assertNativeRulesCutoff(t, cutoff, 2)
}
