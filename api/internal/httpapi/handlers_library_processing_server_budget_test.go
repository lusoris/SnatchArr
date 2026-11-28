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
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

func withServerWriteTimeout(r *http.Request, d time.Duration) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{WriteTimeout: d}))
}

// rulesBudgetNativeWriter models the native writer. The cancel setter runs on
// an AfterFunc goroutine, so cutoff and calls are guarded by mu. Tests read
// them directly only after the handler returned (it joins the setter first) or
// from the handler goroutine while no cancellation is in flight.
type rulesBudgetNativeWriter struct {
	*httptest.ResponseRecorder
	mu           sync.Mutex
	cutoff       time.Time
	calls        []time.Time
	interrupt    chan struct{}
	interruptErr error
}

func newRulesBudgetNativeWriter(cutoff time.Time) *rulesBudgetNativeWriter {
	return &rulesBudgetNativeWriter{ResponseRecorder: httptest.NewRecorder(), cutoff: cutoff}
}

func (w *rulesBudgetNativeWriter) SetWriteDeadline(d time.Time) error {
	w.mu.Lock()
	w.calls = append(w.calls, d)
	if w.interruptErr == nil {
		w.cutoff = d
	}
	w.mu.Unlock()
	if w.interrupt != nil && !d.IsZero() && d.Before(time.Now()) {
		select {
		case w.interrupt <- struct{}{}:
		default:
		}
	}
	return w.interruptErr
}

func (w *rulesBudgetNativeWriter) assertPreserved(t *testing.T, cutoff time.Time) {
	t.Helper()
	if len(w.calls) != 0 || !w.cutoff.Equal(cutoff) {
		t.Fatalf("deadline calls = %v, native cutoff = %v; want no calls and cutoff %v", w.calls, w.cutoff, cutoff)
	}
}

func (w *rulesBudgetNativeWriter) assertInterrupted(t *testing.T, started time.Time) {
	t.Helper()
	if len(w.calls) != 1 || w.calls[0].IsZero() || !w.calls[0].Before(started) {
		t.Fatalf("deadline calls = %v; want one nonzero past deadline", w.calls)
	}
}

func (w *rulesBudgetNativeWriter) waitForInterrupt() error {
	select {
	case <-w.interrupt:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("blocked rules I/O was not interrupted")
	}
}

// Hide the native setter from the streaming handler, as middleware can do.
type rulesBudgetOpaqueWriter struct {
	http.ResponseWriter
	write func(http.ResponseWriter, []byte) (int, error)
	flush func() error
}

func (w rulesBudgetOpaqueWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(w.ResponseWriter, p)
	}
	return w.ResponseWriter.Write(p)
}

func (w rulesBudgetOpaqueWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func rulesBudgetTransport(next http.HandlerFunc, hooks rulesBudgetOpaqueWriter) http.HandlerFunc {
	return WithNativeRulesWriteCancellation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := hooks
		wrapped.ResponseWriter = w
		next(wrapped, r)
	})).ServeHTTP
}

const (
	rulesBudgetSnapshotContract          = 5 * time.Second
	rulesBudgetDefaultServerWriteTimeout = 60 * time.Second
)

type rulesBudgetEndToEndCase struct {
	name           string
	server, caller time.Duration
}

func TestServeLibraryProcessingRulesServerWriteTimeoutEndToEnd(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	for _, tc := range []rulesBudgetEndToEndCase{
		{"default native cutoff", rulesBudgetDefaultServerWriteTimeout, 0},
		{"looser caller leaves native cutoff intact", rulesBudgetDefaultServerWriteTimeout, time.Hour},
		{"caller deadline after snapshot budget", rulesBudgetDefaultServerWriteTimeout, 20 * time.Second},
		{"caller deadline caps snapshot budget", rulesBudgetDefaultServerWriteTimeout, 2 * time.Second},
		{"disabled server timeout", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t, h)
		})
	}
}

func (tc rulesBudgetEndToEndCase) run(t *testing.T, h *Handlers) {
	t.Helper()
	ctx := t.Context()
	var caller, cutoff time.Time
	if tc.caller != 0 {
		caller = time.Now().Add(tc.caller)
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, caller)
		defer cancel()
	}
	if tc.server != 0 {
		cutoff = time.Now().Add(tc.server)
	}
	dw := newRulesBudgetNativeWriter(cutoff)
	req := withServerWriteTimeout(httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil), tc.server)
	var snapshotDeadline time.Time
	var snapshotParentErr func() error
	streamer := rulesBudgetEndToEndStreamer(t, dw, cutoff, &snapshotDeadline)
	handler := h.serveLibraryProcessingRulesWith(2, streamer)
	if tc.server == 0 {
		handler = h.serveLibraryProcessingRulesAt(2, func(parent context.Context) (context.Context, context.CancelFunc) {
			snapshotParentErr = parent.Err
			return context.WithTimeout(parent, rulesBudgetSnapshotContract)
		}, streamer)
	}
	before := time.Now()
	rulesBudgetTransport(handler, rulesBudgetOpaqueWriter{})(dw, req)
	assertRulesBudgetSnapshotDeadline(t, caller, before, snapshotDeadline)
	if tc.server == 0 {
		assertRulesBudgetSnapshotParentAlive(t, snapshotParentErr)
	}
	dw.assertPreserved(t, cutoff)
	assertRulesBudgetEndToEndResponse(t, dw)
}

func rulesBudgetEndToEndStreamer(t *testing.T, dw *rulesBudgetNativeWriter, cutoff time.Time, snapshotDeadline *time.Time) func(context.Context, int, func([]libraryprocessing.StoredRule) error) error {
	t.Helper()
	return func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		*snapshotDeadline, _ = ctx.Deadline()
		for batch := range 3 {
			if err := onBatch(sampleStoredRules(5)[batch*2 : min(batch*2+2, 5)]); err != nil {
				return err
			}
			dw.assertPreserved(t, cutoff)
		}
		return nil
	}
}

func assertRulesBudgetSnapshotDeadline(t *testing.T, caller, before, snapshotDeadline time.Time) {
	t.Helper()
	lo, hi := before.Add(rulesBudgetSnapshotContract), time.Now().Add(rulesBudgetSnapshotContract)
	if !caller.IsZero() && caller.Before(lo) {
		lo, hi = caller, caller
	}
	if snapshotDeadline.Before(lo) || snapshotDeadline.After(hi) {
		t.Fatalf("snapshot deadline = %v, want in [%v, %v]", snapshotDeadline, lo, hi)
	}
}

func assertRulesBudgetSnapshotParentAlive(t *testing.T, snapshotParentErr func() error) {
	t.Helper()
	if snapshotParentErr == nil {
		t.Fatal("snapshot parent is missing after disabled-timeout response")
	}
	if parentErr := snapshotParentErr(); parentErr != nil {
		t.Fatalf("snapshot parent canceled after disabled-timeout response: %v", parentErr)
	}
}

func assertRulesBudgetEndToEndResponse(t *testing.T, dw *rulesBudgetNativeWriter) {
	t.Helper()
	list, err := decodeRuleListJSON(dw.Body.Bytes())
	if dw.Code != http.StatusOK || !dw.Flushed || err != nil || len(list) != 5 {
		t.Fatalf("status = %d, flushed = %v, decoded len = %d, err = %v; body: %s", dw.Code, dw.Flushed, len(list), err, dw.Body.String())
	}
}

func TestServeLibraryProcessingRulesServerWriteTimeoutSnapshotExpiry(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	fixtures := sampleStoredRules(3)
	snapDeadline := time.Now().Add(rulesBudgetSnapshotContract)
	var expSnap *expiringSnapshotCtx
	withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
		var cancel context.CancelFunc
		expSnap, cancel = newExpiringSnapshotCtx(parent, snapDeadline)
		return expSnap, cancel
	}

	cutoff := time.Now().Add(rulesBudgetDefaultServerWriteTimeout)
	dw := newRulesBudgetNativeWriter(cutoff)
	streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
			return err
		}
		dw.assertPreserved(t, cutoff)
		expSnap.expire()
		return nil
	}

	req := withServerWriteTimeout(
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil),
		rulesBudgetDefaultServerWriteTimeout,
	)
	rulesBudgetTransport(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), rulesBudgetOpaqueWriter{})(dw, req)
	if !errors.Is(expSnap.Err(), context.DeadlineExceeded) {
		t.Fatalf("snapshotCtx.Err() = %v, want context.DeadlineExceeded even after server response cancel", expSnap.Err())
	}
	if dw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
	}
	list, err := decodeRuleListJSON(dw.Body.Bytes())
	if err != nil || len(list) != 3 {
		t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
	}
	dw.assertPreserved(t, cutoff)
}

func TestServeLibraryProcessingRulesServerWriteTimeoutParentCancellation(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	// Pre-header cancellation returns RFC 9457 problem+json.
	preCtx, preCancel := context.WithCancel(t.Context())
	defer preCancel()
	cutoff := time.Now().Add(rulesBudgetDefaultServerWriteTimeout)
	dwPre := newRulesBudgetNativeWriter(cutoff)
	preStream := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := fakeBatchStreamer(sampleStoredRules(2))(ctx, batchSize, onBatch); err != nil {
			return err
		}
		preCancel()
		return nil
	}
	reqPre := withServerWriteTimeout(
		httptest.NewRequestWithContext(preCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil),
		rulesBudgetDefaultServerWriteTimeout,
	)
	rulesBudgetTransport(h.serveLibraryProcessingRulesWith(2, preStream), rulesBudgetOpaqueWriter{})(dwPre, reqPre)
	dwPre.assertPreserved(t, cutoff)
	assertProblemResponse(t, dwPre.ResponseRecorder, http.StatusInternalServerError)

	// Post-header cancellation after intermediate flush aborts incomplete JSON stream and keeps server deadline.
	postCtx, postCancel := context.WithCancel(t.Context())
	defer postCancel()
	dwPost := newRulesBudgetNativeWriter(cutoff)
	postStream := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
			return err
		}
		postCancel()
		return nil
	}
	reqPost := withServerWriteTimeout(
		httptest.NewRequestWithContext(postCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil),
		rulesBudgetDefaultServerWriteTimeout,
	)
	aborted := invokeCapturingAbort(rulesBudgetTransport(h.serveLibraryProcessingRulesWith(2, postStream), rulesBudgetOpaqueWriter{}), dwPost, reqPost)
	if !aborted {
		t.Fatalf("expected ErrAbortHandler when parent cancels post-header; body: %s", dwPost.Body.String())
	}
	dwPost.assertPreserved(t, cutoff)
	var shouldFail []any
	if json.Unmarshal(dwPost.Body.Bytes(), &shouldFail) == nil {
		t.Fatalf("canceled stream parsed as complete JSON array: %s", dwPost.Body.String())
	}
}

func TestServeLibraryProcessingRulesServerWriteTimeoutInterruptsIntermediateFlush(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	started := time.Now()
	cutoff := started.Add(rulesBudgetDefaultServerWriteTimeout)
	dw := newRulesBudgetNativeWriter(cutoff)
	dw.interrupt = make(chan struct{}, 1)
	boom := errors.New("native write cancellation failed")
	dw.interruptErr = boom
	var snap *expiringSnapshotCtx
	withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
		var cancel context.CancelFunc
		snap, cancel = newExpiringSnapshotCtx(parent, started.Add(rulesBudgetSnapshotContract))
		return snap, cancel
	}
	var batchErr error
	streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		batchErr = fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch)
		return batchErr
	}
	hooks := rulesBudgetOpaqueWriter{flush: func() error {
		snap.expire()
		return dw.waitForInterrupt()
	}}
	req := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil), rulesBudgetDefaultServerWriteTimeout)
	if !invokeCapturingAbort(rulesBudgetTransport(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), hooks), dw, req) {
		t.Fatalf("expected abort during intermediate flush; body: %s", dw.Body.String())
	}
	if !errors.Is(batchErr, context.DeadlineExceeded) || !errors.Is(batchErr, boom) {
		t.Fatalf("batch error = %v, want snapshot expiry and setter failure", batchErr)
	}
	dw.assertInterrupted(t, started)
	if !dw.cutoff.Equal(cutoff) || dw.Body.Len() == 0 || json.Valid(dw.Body.Bytes()) {
		t.Fatalf("native cutoff = %v, body = %q; want unchanged cutoff and partial JSON", dw.cutoff, dw.Body.String())
	}
}

func TestServeLibraryProcessingRulesServerWriteTimeoutInterruptsFinalWrite(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := time.Now()
	dw := newRulesBudgetNativeWriter(started.Add(rulesBudgetDefaultServerWriteTimeout))
	dw.interrupt = make(chan struct{}, 1)
	var snap *expiringSnapshotCtx
	withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
		var cleanup context.CancelFunc
		snap, cleanup = newExpiringSnapshotCtx(parent, started.Add(rulesBudgetSnapshotContract))
		return snap, cleanup
	}
	snapshotDone := false
	streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
			return err
		}
		snap.expire()
		snapshotDone = true
		return nil
	}
	var finalInterrupted bool
	hooks := rulesBudgetOpaqueWriter{write: func(w http.ResponseWriter, p []byte) (int, error) {
		if !snapshotDone {
			return w.Write(p)
		}
		cancel()
		if err := dw.waitForInterrupt(); err != nil {
			return 0, err
		}
		finalInterrupted = true
		return 0, context.Canceled
	}}
	req := withServerWriteTimeout(httptest.NewRequestWithContext(ctx, http.MethodGet, PathPrefix+"/library-processing/rules", nil), rulesBudgetDefaultServerWriteTimeout)
	aborted := invokeCapturingAbort(rulesBudgetTransport(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), hooks), dw, req)
	if !aborted || !finalInterrupted || dw.Body.Len() == 0 || json.Valid(dw.Body.Bytes()) {
		t.Fatalf("aborted = %v, final write interrupted = %v, body = %q; want interrupted final write and partial JSON", aborted, finalInterrupted, dw.Body.String())
	}
	dw.assertInterrupted(t, started)
}

func TestServeLibraryProcessingRulesServerWriteTimeoutResponseExpiry(t *testing.T) {
	t.Parallel()
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	cutoff := time.Now().Add(rulesBudgetDefaultServerWriteTimeout)
	expResp, cancelResp := newExpiringSnapshotCtx(t.Context(), cutoff.Add(-20*time.Second))
	defer cancelResp()
	dw := newRulesBudgetNativeWriter(cutoff)
	var expiryPropagated bool
	streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
			return err
		}
		expResp.expire()
		// The response and snapshot contexts are derived from a custom
		// context, so cancellation reaches them through a goroutine. Wait
		// for the snapshot (the youngest child) instead of racing it.
		select {
		case <-ctx.Done():
			expiryPropagated = true
		case <-time.After(10 * time.Second):
			return errors.New("response expiry did not propagate to the snapshot context")
		}
		return nil
	}
	req := withServerWriteTimeout(httptest.NewRequestWithContext(expResp, http.MethodGet, PathPrefix+"/library-processing/rules", nil), rulesBudgetDefaultServerWriteTimeout)
	aborted := invokeCapturingAbort(rulesBudgetTransport(h.serveLibraryProcessingRulesWith(2, streamer), rulesBudgetOpaqueWriter{}), dw, req)
	if !expiryPropagated {
		t.Fatal("response expiry did not propagate to the snapshot context")
	}
	if !aborted {
		t.Fatalf("expected abort after response expiry; body: %s", dw.Body.String())
	}
	dw.assertPreserved(t, cutoff)
	var shouldFail []any
	if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
		t.Fatalf("expired server budget emitted complete JSON array: %s", dw.Body.String())
	}
}
