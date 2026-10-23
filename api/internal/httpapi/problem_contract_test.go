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
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

type contractExpected struct {
	status        int
	detail        string
	instance      string
	invalidParams []contractInvalidParam
}

type contractInvalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type contractTestCase struct {
	name   string
	err    error
	target string
	want   contractExpected
}

type contractProblemBody struct {
	Type          string                 `json:"type"`
	Title         string                 `json:"title"`
	Status        int                    `json:"status"`
	Detail        string                 `json:"detail"`
	Instance      string                 `json:"instance"`
	InvalidParams []contractInvalidParam `json:"invalid-params,omitempty"` //nolint:tagliatelle // RFC 9457 extension member name is hyphenated
}

func problemContractCases() []contractTestCase {
	validateErr := &validate.Error{
		Fields: []validate.FieldError{
			{Name: "page_size", Error: errors.New("must be at least 1")},
			{Name: "sort_order", Error: errors.New("unsupported sort direction")},
		},
	}
	return []contractTestCase{
		{
			name:   "positive decode request error 400",
			err:    &ogenerrors.DecodeRequestError{Err: errors.New("json: syntax error")},
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusBadRequest,
				detail:   "malformed request",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive decode params error 400",
			err:    &ogenerrors.DecodeParamsError{Err: errors.New("invalid integer")},
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusBadRequest,
				detail:   "malformed request",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive validate error with fields 422",
			err:    validateErr,
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnprocessableEntity,
				detail:   "request validation failed",
				instance: "/api/v1/instances",
				invalidParams: []contractInvalidParam{
					{Name: "page_size", Reason: "must be at least 1"},
					{Name: "sort_order", Reason: "unsupported sort direction"},
				},
			},
		},
		{
			// Body bound violations reach the handler as a decode error wrapping validate.Error.
			name:   "positive decode request error wrapping validate error 422",
			err:    &ogenerrors.DecodeRequestError{Err: validateErr},
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnprocessableEntity,
				detail:   "request validation failed",
				instance: "/api/v1/instances",
				invalidParams: []contractInvalidParam{
					{Name: "page_size", Reason: "must be at least 1"},
					{Name: "sort_order", Reason: "unsupported sort direction"},
				},
			},
		},
		{
			name:   "positive domain invalid wrapped 422",
			err:    fmt.Errorf("instance validation: %w: name too short", domain.ErrInvalid),
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnprocessableEntity,
				detail:   "instance validation: invalid: name too short",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive queue invalid wrapped 422",
			err:    fmt.Errorf("queue rule: %w: grace out of bounds", queue.ErrInvalid),
			target: "/api/v1/instances/1/queue",
			want: contractExpected{
				status:   http.StatusUnprocessableEntity,
				detail:   "queue rule: invalid queue settings: grace out of bounds",
				instance: "/api/v1/instances/1/queue",
			},
		},
		{
			name:   "positive ogenerrors security error 401",
			err:    &ogenerrors.SecurityError{Security: "sessionAuth", Err: errors.New("token expired")},
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnauthorized,
				detail:   "authentication required",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive domain unauthorized wrapped 401",
			err:    fmt.Errorf("auth guard: %w", domain.ErrUnauthorized),
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnauthorized,
				detail:   "authentication required",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive sso proxy secret error 401",
			err:    sso.ErrProxySecret,
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusUnauthorized,
				detail:   "authentication required",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive domain forbidden full msg 403",
			err:    fmt.Errorf("rbac check: %w: read only access", domain.ErrForbidden),
			target: "/api/v1/instances/1",
			want: contractExpected{
				status:   http.StatusForbidden,
				detail:   "rbac check: forbidden: read only access",
				instance: "/api/v1/instances/1",
			},
		},
		{
			name:   "positive domain too many requests 429",
			err:    fmt.Errorf("rate limiter: %w: cap exhausted", domain.ErrTooManyRequests),
			target: "/api/v1/instances/1/snatches",
			want: contractExpected{
				status:   http.StatusTooManyRequests,
				detail:   "too many requests",
				instance: "/api/v1/instances/1/snatches",
			},
		},
		{
			name:   "positive domain not found full msg 404",
			err:    fmt.Errorf("storage lookup: %w: instance 999", domain.ErrNotFound),
			target: "/api/v1/instances/999",
			want: contractExpected{
				status:   http.StatusNotFound,
				detail:   "storage lookup: not found: instance 999",
				instance: "/api/v1/instances/999",
			},
		},
		{
			name:   "positive domain conflict full msg 409",
			err:    fmt.Errorf("create instance: %w: name exists", domain.ErrConflict),
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusConflict,
				detail:   "create instance: conflict: name exists",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive domain bad request full msg 400",
			err:    fmt.Errorf("params parser: %w: bad cursor", domain.ErrBadRequest),
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusBadRequest,
				detail:   "params parser: bad request: bad cursor",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "positive domain unreachable full msg 502",
			err:    fmt.Errorf("arr proxy: %w: dial tcp connection refused", domain.ErrUnreachable),
			target: "/api/v1/instances/1/test",
			want: contractExpected{
				status:   http.StatusBadGateway,
				detail:   "arr proxy: instance unreachable: dial tcp connection refused",
				instance: "/api/v1/instances/1/test",
			},
		},
		{
			name:   "negative unknown error maps to 500",
			err:    errors.New("database disk full failure"),
			target: "/api/v1/instances",
			want: contractExpected{
				status:   http.StatusInternalServerError,
				detail:   "internal error",
				instance: "/api/v1/instances",
			},
		},
		{
			name:   "boundary request with query string strips query from instance",
			err:    errors.New("internal failure"),
			target: "/api/v1/instances?page=2&limit=50&filter=active",
			want: contractExpected{
				status:   http.StatusInternalServerError,
				detail:   "internal error",
				instance: "/api/v1/instances",
			},
		},
	}
}

func checkProblemHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}

func checkProblemFields(t *testing.T, body contractProblemBody, want contractExpected) {
	t.Helper()
	if body.Type != "about:blank" {
		t.Errorf("type = %q, want about:blank", body.Type)
	}
	if body.Title != http.StatusText(want.status) {
		t.Errorf("title = %q, want %q", body.Title, http.StatusText(want.status))
	}
	if body.Status != want.status {
		t.Errorf("status = %d, want %d", body.Status, want.status)
	}
	if body.Detail != want.detail {
		t.Errorf("detail = %q, want %q", body.Detail, want.detail)
	}
	if body.Instance != want.instance {
		t.Errorf("instance = %q, want %q", body.Instance, want.instance)
	}
	checkInvalidParams(t, body.InvalidParams, want.invalidParams)
}

func checkInvalidParams(t *testing.T, got, want []contractInvalidParam) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("invalid-params count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Reason != want[i].Reason {
			t.Errorf("invalid-params[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestProblemContract(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	handler := ErrorHandler(logger)

	for _, tc := range problemContractCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)

			handler(context.Background(), rec, req, tc.err)

			if rec.Code != tc.want.status {
				t.Fatalf("HTTP code = %d, want %d", rec.Code, tc.want.status)
			}
			checkProblemHeaders(t, rec)

			var body contractProblemBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode problem body: %v; raw: %s", err, rec.Body.String())
			}
			checkProblemFields(t, body, tc.want)
		})
	}
}
