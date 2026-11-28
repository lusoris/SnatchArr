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
