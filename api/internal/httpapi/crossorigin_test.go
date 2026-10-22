// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golusoris/golusoris/ogenkit"
)

func TestCrossOriginContract(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	mw := CrossOrigin(logger)

	t.Run("positive same-origin allowed", func(t *testing.T) {
		t.Parallel()
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := mw(next)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("positive cross-site refused 403 problem contract", func(t *testing.T) {
		t.Parallel()
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := mw(next)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("Content-Type = %q, want application/problem+json", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", cc)
		}

		var body ogenkit.Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode problem body: %v; raw: %s", err, rec.Body.String())
		}
		if body.Type != "about:blank" {
			t.Errorf("type = %q, want about:blank", body.Type)
		}
		if body.Title != "Forbidden" {
			t.Errorf("title = %q, want Forbidden", body.Title)
		}
		if body.Status != http.StatusForbidden {
			t.Errorf("status = %d, want %d", body.Status, http.StatusForbidden)
		}
		if body.Detail != "cross-origin request refused" {
			t.Errorf("detail = %q, want cross-origin request refused", body.Detail)
		}
		if body.Instance != "/api/v1/instances" {
			t.Errorf("instance = %q, want /api/v1/instances", body.Instance)
		}
	})

	t.Run("boundary sentinel error resolve", func(t *testing.T) {
		t.Parallel()
		p := resolver.Resolve(nil, errCrossOrigin)
		if p.Status != http.StatusForbidden {
			t.Errorf("status = %d, want %d", p.Status, http.StatusForbidden)
		}
		if p.Detail != "cross-origin request refused" {
			t.Errorf("detail = %q, want cross-origin request refused", p.Detail)
		}
	})
}
