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
