// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestStatusOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"positive not found", fmt.Errorf("wrap: %w", domain.ErrNotFound), http.StatusNotFound},
		{"positive conflict", domain.ErrConflict, http.StatusConflict},
		{"positive invalid", fmt.Errorf("%w: bad", domain.ErrInvalid), http.StatusUnprocessableEntity},
		{"positive unauthorized", domain.ErrUnauthorized, http.StatusUnauthorized},
		{"positive forbidden", domain.ErrForbidden, http.StatusForbidden},
		{"positive unreachable", domain.ErrUnreachable, http.StatusBadGateway},
		{"positive ogen security", &ogenerrors.SecurityError{Security: "cookieAuth", Err: errors.New("no")}, http.StatusUnauthorized},
		{"positive ogen validation", &validate.Error{Fields: []validate.FieldError{{Name: "hourly_cap", Error: errors.New("too big")}}}, http.StatusUnprocessableEntity},
		{"negative unknown is 500", errors.New("boom"), http.StatusInternalServerError},
		{"boundary nil-ish wrapped internal", fmt.Errorf("db: %w", errors.New("x")), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, _ := statusOf(tc.err)
			if got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrorHandlerWritesProblemJSON(t *testing.T) {
	t.Parallel()
	h := ErrorHandler(slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/x", nil)
	h(context.Background(), rec, req, &validate.Error{Fields: []validate.FieldError{{Name: "page_size", Error: errors.New("min")}}})

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q", ct)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	var body problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != 422 || body.Instance != "/api/v1/instances/x" || len(body.InvalidParams) != 1 {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body.InvalidParams[0].Name != "page_size" {
		t.Fatalf("invalid-params = %+v", body.InvalidParams)
	}
}

func TestErrorHandlerHidesInternalDetail(t *testing.T) {
	t.Parallel()
	h := ErrorHandler(slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h(context.Background(), rec, req, errors.New("password=snatcher2 leaked"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); contains(got, "snatcher2") {
		t.Fatalf("internal detail leaked: %s", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
