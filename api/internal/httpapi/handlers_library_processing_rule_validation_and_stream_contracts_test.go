// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

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

	t.Run("empty stream writes non-nil JSON array after commit without touching the native cutoff", func(t *testing.T) {
		t.Parallel()
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(nil)), dw, req) {
			t.Fatal("empty stream aborted")
		}

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || list == nil || len(list) != 0 {
			t.Fatalf("empty body %q -> (%#v, %v), want non-nil []", dw.Body.String(), list, err)
		}
		// The closing "[]" is the only chunk: one write and one flush.
		dw.assertNativeRulesCutoff(t, cutoff, 1)
	})

	t.Run("multi-batch stream leaves the native cutoff untouched through commit and finish", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(5)
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
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
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatal("successful stream aborted")
		}

		if dw.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", dw.Code, dw.Body.String())
		}
		if !seenAtCommit || !deadlineAtCommit.Equal(cutoff) {
			t.Fatalf("native cutoff at SnapshotReadTx commit = %v (seen=%v), want unchanged %v", deadlineAtCommit, seenAtCommit, cutoff)
		}
		list, err := decodeRuleListJSON(dw.Body.Bytes())
		if err != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, dw.Body.String())
		}
		// 5 items with batchSize=2 -> 3 batches -> 2 intermediate chunks inside tx + 1 final chunk after commit.
		dw.assertNativeRulesCutoff(t, cutoff, 3)
	})

	t.Run("committed multi-batch stream finishes last batch when snapshot clock expires before finish", func(t *testing.T) {
		t.Parallel()
		fixtures := sampleStoredRules(3)
		reqCtx, reqCancel := context.WithTimeout(t.Context(), time.Hour)
		defer reqCancel()
		snapDeadline := time.Now().Add(snapshotContract)
		var expSnap *expiringSnapshotCtx
		withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
			var cancel context.CancelFunc
			expSnap, cancel = newExpiringSnapshotCtx(parent, snapDeadline)
			return expSnap, cancel
		}

		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(30 * time.Minute)
		dw.currentDeadline = cutoff
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			if !dw.currentDeadline.Equal(cutoff) {
				t.Errorf("native cutoff at commit = %v, want unchanged %v", dw.currentDeadline, cutoff)
			}
			// SnapshotReadTx has committed; expire only the snapshot clock before finish writes batch 2.
			expSnap.expire()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), dw, req) {
			t.Fatal("final chunk aborted after only the snapshot clock expired")
		}

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
		// 3 items -> 1 intermediate chunk + 1 final chunk; snapshot expiry after commit must not reach the writer.
		dw.assertNativeRulesCutoff(t, cutoff, 2)
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
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(fixtures)(ctx, batchSize, onBatch); err != nil {
				return err
			}
			expSnap.expire()
			return nil
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesAt(2, withSnap, streamer), dw, req) {
			t.Fatal("single final chunk aborted after only the snapshot clock expired")
		}

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
		// One batch stays buffered, so the final chunk is the only write and flush.
		dw.assertNativeRulesCutoff(t, cutoff, 1)
	})

	t.Run("request cancellation after commit before finish aborts post-header multi-batch stream", func(t *testing.T) {
		t.Parallel()
		reqCtx, reqCancel := context.WithCancel(t.Context())
		defer reqCancel()
		dw := newDeadlineRecordingWriter()
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(3))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			// SnapshotReadTx committed after flushing batch 1; caller disconnects before final write.
			reqCancel()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		aborted := invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req)
		if !aborted {
			t.Fatalf("expected ErrAbortHandler when caller cancels after commit; body: %s", dw.Body.String())
		}
		// The final chunk is rejected before any I/O, so only batch 1 was written.
		if dw.writeCalls != 1 || dw.flushCalls != 1 {
			t.Fatalf("writes=%d flushes=%d, want only the intermediate chunk", dw.writeCalls, dw.flushCalls)
		}
		dw.assertNativeCutoffPreserved(t, cutoff)
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
		cutoff := time.Now().Add(time.Minute)
		dw.currentDeadline = cutoff
		streamer := func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			if err := fakeBatchStreamer(sampleStoredRules(2))(ctx, batchSize, onBatch); err != nil {
				return err
			}
			// Only 1 batch buffered in memory; headers have not been committed yet.
			reqCancel()
			return nil
		}

		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
		if invokeRulesWithNativeCancellation(h.serveLibraryProcessingRulesWith(2, streamer), dw, req) {
			t.Fatal("pre-commit failure aborted instead of returning a problem response")
		}
		assertProblemResponse(t, dw.ResponseRecorder, http.StatusInternalServerError)
		// The only write is the problem body itself; no stream chunk, no flush.
		if dw.writeCalls != 1 || dw.flushCalls != 0 {
			t.Fatalf("writes=%d flushes=%d, want only the problem body write", dw.writeCalls, dw.flushCalls)
		}
		dw.assertNativeCutoffPreserved(t, cutoff)
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
