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
	"slices"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

func withServerWriteTimeout(r *http.Request, d time.Duration) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{WriteTimeout: d}))
}

func TestRuleStreamWriteDeadlines(t *testing.T) {
	t.Parallel()
	const defaultServerWriteTimeout = 60 * time.Second

	t.Run("default 60s server budget caps final output and restores after snapshot flush", func(t *testing.T) {
		t.Parallel()
		r := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), defaultServerWriteTimeout)
		before := time.Now()
		responseCtx, cancelResponse := ruleStreamResponseContext(r.Context())
		defer cancelResponse()
		after := time.Now()
		serverLo := before.Add(defaultServerWriteTimeout)
		serverHi := after.Add(defaultServerWriteTimeout)
		snapshot := before.Add(5 * time.Second)
		snapshotCtx, cancelSnapshot := context.WithDeadline(responseCtx, snapshot)
		defer cancelSnapshot()

		dw := newDeadlineRecordingWriter()
		deadline, ok := responseCtx.Deadline()
		if !ok || deadline.Before(serverLo) || deadline.After(serverHi) {
			t.Fatalf("response deadline = %v (ok=%v), want in [%v, %v]", deadline, ok, serverLo, serverHi)
		}
		sw := &ruleArrayStreamWriter{w: dw, responseDeadline: deadline}
		if err := sw.flushPending(snapshotCtx); err != nil {
			t.Fatal(err)
		}
		if len(dw.deadlines) != 2 || !dw.deadlines[0].Equal(snapshot) || dw.deadlines[1].Before(serverLo) || dw.deadlines[1].After(serverHi) {
			t.Fatalf("intermediate deadline calls = %v, want [%v, server in [%v, %v]]", dw.deadlines, snapshot, serverLo, serverHi)
		}
		restored := dw.deadlines[1]
		dw.deadlines = nil
		cancelSnapshot()
		if err := sw.finish(responseCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{restored}) {
			t.Fatalf("final deadline calls = %v, want [%v]", dw.deadlines, restored)
		}
		if !dw.currentDeadline.Equal(restored) {
			t.Fatalf("currentDeadline = %v, want server deadline %v", dw.currentDeadline, restored)
		}
		if got := dw.Body.String(); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})

	t.Run("default 60s server budget independently caps looser caller deadline", func(t *testing.T) {
		t.Parallel()
		baseReq := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), defaultServerWriteTimeout)
		callerCtx, cancelCaller := context.WithTimeout(baseReq.Context(), time.Hour)
		defer cancelCaller()
		before := time.Now()
		responseCtx, cancelResponse := ruleStreamResponseContext(callerCtx)
		defer cancelResponse()
		after := time.Now()
		serverLo := before.Add(defaultServerWriteTimeout)
		serverHi := after.Add(defaultServerWriteTimeout)
		snapshot := before.Add(5 * time.Second)
		snapshotCtx, cancelSnapshot := context.WithDeadline(responseCtx, snapshot)
		defer cancelSnapshot()

		dw := newDeadlineRecordingWriter()
		deadline, ok := responseCtx.Deadline()
		if !ok || deadline.Before(serverLo) || deadline.After(serverHi) {
			t.Fatalf("response deadline = %v (ok=%v), want capped at server [%v, %v]", deadline, ok, serverLo, serverHi)
		}
		sw := &ruleArrayStreamWriter{w: dw, responseDeadline: deadline}
		if err := sw.flushPending(snapshotCtx); err != nil {
			t.Fatal(err)
		}
		if len(dw.deadlines) != 2 || !dw.deadlines[0].Equal(snapshot) || !dw.deadlines[1].Equal(deadline) {
			t.Fatalf("intermediate deadline calls = %v, want [%v, %v]", dw.deadlines, snapshot, deadline)
		}
		dw.deadlines = nil
		cancelSnapshot()
		if err := sw.finish(responseCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{deadline}) {
			t.Fatalf("final deadline calls = %v, want [%v]", dw.deadlines, deadline)
		}
		if got := dw.Body.String(); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})

	t.Run("earlier caller deadline above snapshot budget restores and caps final output", func(t *testing.T) {
		t.Parallel()
		baseReq := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), defaultServerWriteTimeout)
		entered := time.Now()
		caller := entered.Add(20 * time.Second)
		snapshot := entered.Add(5 * time.Second)
		callerCtx, cancelCaller := context.WithDeadline(baseReq.Context(), caller)
		defer cancelCaller()
		responseCtx, cancelResponse := ruleStreamResponseContext(callerCtx)
		defer cancelResponse()
		snapshotCtx, cancelSnapshot := context.WithDeadline(responseCtx, snapshot)
		defer cancelSnapshot()

		dw := newDeadlineRecordingWriter()
		deadline, _ := responseCtx.Deadline()
		sw := &ruleArrayStreamWriter{w: dw, responseDeadline: deadline}
		if err := sw.flushPending(snapshotCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{snapshot, caller}) {
			t.Fatalf("intermediate deadline calls = %v, want [%v, %v]", dw.deadlines, snapshot, caller)
		}
		dw.deadlines = nil
		cancelSnapshot()
		if err := sw.finish(responseCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{caller}) {
			t.Fatalf("final deadline calls = %v, want [%v]", dw.deadlines, caller)
		}
		if !dw.currentDeadline.Equal(caller) {
			t.Fatalf("currentDeadline = %v, want %v", dw.currentDeadline, caller)
		}
		if got := dw.Body.String(); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})

	t.Run("earlier caller deadline below snapshot budget caps both intermediate and final writes", func(t *testing.T) {
		t.Parallel()
		baseReq := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), defaultServerWriteTimeout)
		entered := time.Now()
		caller := entered.Add(2 * time.Second)
		snapshot := entered.Add(5 * time.Second)
		callerCtx, cancelCaller := context.WithDeadline(baseReq.Context(), caller)
		defer cancelCaller()
		responseCtx, cancelResponse := ruleStreamResponseContext(callerCtx)
		defer cancelResponse()
		snapshotCtx, cancelSnapshot := context.WithDeadline(responseCtx, snapshot)
		defer cancelSnapshot()

		dw := newDeadlineRecordingWriter()
		deadline, _ := responseCtx.Deadline()
		sw := &ruleArrayStreamWriter{w: dw, responseDeadline: deadline}
		if err := sw.flushPending(snapshotCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{caller}) {
			t.Fatalf("intermediate deadline calls = %v, want [%v]", dw.deadlines, caller)
		}
		dw.deadlines = nil
		cancelSnapshot()
		if err := sw.finish(responseCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{caller}) {
			t.Fatalf("final deadline calls = %v, want [%v]", dw.deadlines, caller)
		}
		if got := dw.Body.String(); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})

	t.Run("disabled server timeout clears intermediate deadline to zero and does not cancel response context on cleanup", func(t *testing.T) {
		t.Parallel()
		r := withServerWriteTimeout(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), 0)
		snapshot := time.Now().Add(5 * time.Second)
		responseCtx, cancelResponse := ruleStreamResponseContext(r.Context())
		snapshotCtx, cancelSnapshot := context.WithDeadline(responseCtx, snapshot)
		defer cancelSnapshot()

		dw := newDeadlineRecordingWriter()
		deadline, _ := responseCtx.Deadline()
		sw := &ruleArrayStreamWriter{w: dw, responseDeadline: deadline}
		if err := sw.flushPending(snapshotCtx); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dw.deadlines, []time.Time{snapshot, {}}) {
			t.Fatalf("intermediate deadline calls = %v, want [%v, zero]", dw.deadlines, snapshot)
		}
		dw.deadlines = nil
		cancelSnapshot()
		if err := sw.finish(responseCtx); err != nil {
			t.Fatal(err)
		}
		if len(dw.deadlines) != 0 || !dw.currentDeadline.IsZero() {
			t.Fatalf("final deadline calls = %v (current=%v), want none and zero", dw.deadlines, dw.currentDeadline)
		}
		cancelResponse()
		if err := responseCtx.Err(); err != nil {
			t.Fatalf("responseCtx.Err() after cancelResponse = %v, want nil when no server timeout applied", err)
		}
		if got := dw.Body.String(); got != "[]" {
			t.Fatalf("body = %q, want []", got)
		}
	})
}

func TestServeLibraryProcessingRulesServerWriteTimeoutEndToEnd(t *testing.T) {
	t.Parallel()
	const (
		snapshotContract          = 5 * time.Second
		defaultServerWriteTimeout = 60 * time.Second
	)
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk)}

	t.Run("default 60s server WriteTimeout restores 60s across intermediate flushes and bounds finish", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(5)
		dw := newDeadlineRecordingWriter()
		var deadlineAtCommit time.Time
		seenAtCommit := false
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			deadlineAtCommit = dw.currentDeadline
			seenAtCommit = true
			return nil
		}

		req := withServerWriteTimeout(
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			defaultServerWriteTimeout,
		)
		before := time.Now()
		h.serveLibraryProcessingRulesWith(2, streamer)(dw, req)
		after := time.Now()

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		serverLo := before.Add(defaultServerWriteTimeout)
		serverHi := after.Add(defaultServerWriteTimeout)
		if !seenAtCommit || deadlineAtCommit.Before(serverLo) || deadlineAtCommit.After(serverHi) {
			t.Fatalf("deadline armed at commit = %v (seen=%v), want server deadline in [%v, %v]", deadlineAtCommit, seenAtCommit, serverLo, serverHi)
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		dw.assertStreamDeadlines(t, before.Add(snapshotContract), after.Add(snapshotContract), serverLo, serverHi, 2)
	})

	t.Run("default 60s server WriteTimeout finishes after deterministic snapshot expiry and keeps snapshot Err stable", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(3)
		snapDeadline := time.Now().Add(snapshotContract)
		var expSnap *expiringSnapshotCtx
		withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
			var cancel context.CancelFunc
			expSnap, cancel = newExpiringSnapshotCtx(parent, snapDeadline)
			return expSnap, cancel
		}

		dw := newDeadlineRecordingWriter()
		var commitDeadline time.Time
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			commitDeadline = dw.currentDeadline
			expSnap.expire()
			return nil
		}

		req := withServerWriteTimeout(
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			defaultServerWriteTimeout,
		)
		before := time.Now()
		h.serveLibraryProcessingRulesAt(2, withSnap, streamer)(dw, req)
		after := time.Now()

		serverLo := before.Add(defaultServerWriteTimeout)
		serverHi := after.Add(defaultServerWriteTimeout)
		if commitDeadline.Before(serverLo) || commitDeadline.After(serverHi) {
			t.Fatalf("commitDeadline = %v, want in [%v, %v]", commitDeadline, serverLo, serverHi)
		}
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
		dw.assertStreamDeadlines(t, snapDeadline, snapDeadline, serverLo, serverHi, 1)
	})

	t.Run("parent request cancellation propagates through 60s server response context before and after headers", func(t *testing.T) {
		t.Parallel()
		// Pre-header cancellation returns RFC 9457 problem+json.
		preCtx, preCancel := context.WithCancel(t.Context())
		defer preCancel()
		dwPre := newDeadlineRecordingWriter()
		preStream := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(2))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			preCancel()
			return nil
		}
		reqPre := withServerWriteTimeout(
			httptest.NewRequestWithContext(preCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			defaultServerWriteTimeout,
		)
		h.serveLibraryProcessingRulesWith(2, preStream)(dwPre, reqPre)
		assertProblemResponse(t, dwPre.ResponseRecorder, http.StatusInternalServerError)

		// Post-header cancellation after intermediate flush aborts incomplete JSON stream and keeps server deadline.
		postCtx, postCancel := context.WithCancel(t.Context())
		defer postCancel()
		dwPost := newDeadlineRecordingWriter()
		postStream := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			postCancel()
			return nil
		}
		reqPost := withServerWriteTimeout(
			httptest.NewRequestWithContext(postCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			defaultServerWriteTimeout,
		)
		before := time.Now()
		aborted := invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, postStream), dwPost, reqPost)
		after := time.Now()
		if !aborted {
			t.Fatalf("expected ErrAbortHandler when parent cancels post-header; body: %s", dwPost.Body.String())
		}
		if dwPost.currentDeadline.Before(before.Add(defaultServerWriteTimeout)) || dwPost.currentDeadline.After(after.Add(defaultServerWriteTimeout)) {
			t.Fatalf("currentDeadline = %v, want preserved 60s server deadline", dwPost.currentDeadline)
		}
		var shouldFail []any
		if json.Unmarshal(dwPost.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("canceled stream parsed as complete JSON array: %s", dwPost.Body.String())
		}
	})

	t.Run("restore 60s server deadline failure in intermediate flushPending aborts before commit", func(t *testing.T) {
		t.Parallel()
		restoreBoom := errors.New("restore 60s server deadline broken")
		dw := newDeadlineRecordingWriter()
		restoreCalls := 0
		dw.setDeadlineErr = func(call int, _ time.Time) error {
			if call == 2 {
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
		req := withServerWriteTimeout(
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil),
			defaultServerWriteTimeout,
		)
		if !invokeCapturingAbort(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatalf("expected ErrAbortHandler on server deadline restore failure; body: %s", dw.Body.String())
		}
		if reachedCommit || restoreCalls != 1 {
			t.Fatalf("reachedCommit=%v restoreCalls=%d, want (false, 1)", reachedCommit, restoreCalls)
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("partial JSON unexpectedly complete: %s", dw.Body.String())
		}
	})

	t.Run("expired 60s server response deadline after commit rejects final output", func(t *testing.T) {
		t.Parallel()
		serverDeadline := time.Now().Add(defaultServerWriteTimeout)
		expResp, cancelResp := newExpiringSnapshotCtx(t.Context(), serverDeadline)
		defer cancelResp()
		snapCtx, cancelSnap := context.WithTimeout(expResp, snapshotContract)
		defer cancelSnap()

		dw := newDeadlineRecordingWriter()
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			expResp.expire()
			return nil
		}
		committed, err := streamRuleArray(expResp, snapCtx, dw, 2, streamer)
		if !committed || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("(committed=%v, err=%v), want (true, context.DeadlineExceeded)", committed, err)
		}
		if !dw.currentDeadline.Equal(serverDeadline) {
			t.Fatalf("currentDeadline = %v, want preserved server deadline %v", dw.currentDeadline, serverDeadline)
		}
		var shouldFail []any
		if json.Unmarshal(dw.Body.Bytes(), &shouldFail) == nil {
			t.Fatalf("expired server budget emitted complete JSON array: %s", dw.Body.String())
		}
	})
}
