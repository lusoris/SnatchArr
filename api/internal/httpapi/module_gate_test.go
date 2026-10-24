// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ogen-go/ogen/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/moduleops"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
)

type fakeModuleChecker struct {
	enabled map[string]bool
	err     map[string]error
	calls   int
}

func (f *fakeModuleChecker) IsEnabled(_ context.Context, id string) (bool, error) {
	f.calls++
	if f.err != nil {
		if err, ok := f.err[id]; ok && err != nil {
			return false, err
		}
	}
	if f.enabled != nil {
		return f.enabled[id], nil
	}
	return false, nil
}

func fakeNext(called *bool, resp middleware.Response) middleware.Next {
	return func(_ middleware.Request) (middleware.Response, error) {
		*called = true
		return resp, nil
	}
}

func TestModuleGateMiddleware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		ops           map[string]string
		opID          string
		checker       *fakeModuleChecker
		wantCalled    bool
		wantErrIs     error
		wantErrNotNil bool
	}{
		{
			name:       "positive mapped and enabled",
			ops:        map[string]string{"testOp": "watchaware"},
			opID:       "testOp",
			checker:    &fakeModuleChecker{enabled: map[string]bool{"watchaware": true}},
			wantCalled: true,
		},
		{
			name:          "negative mapped and disabled",
			ops:           map[string]string{"testOp": "watchaware"},
			opID:          "testOp",
			checker:       &fakeModuleChecker{enabled: map[string]bool{"watchaware": false}},
			wantCalled:    false,
			wantErrIs:     domain.ErrNotFound,
			wantErrNotNil: true,
		},
		{
			name:          "negative checker error",
			ops:           map[string]string{"testOp": "watchaware"},
			opID:          "testOp",
			checker:       &fakeModuleChecker{err: map[string]error{"watchaware": errors.New("db error")}},
			wantCalled:    false,
			wantErrNotNil: true,
		},
		{
			name:       "boundary unmapped op",
			ops:        map[string]string{"otherOp": "watchaware"},
			opID:       "testOp",
			checker:    &fakeModuleChecker{enabled: map[string]bool{"watchaware": false}},
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mw := ModuleGateMiddleware(tt.ops, tt.checker)
			var called bool
			req := middleware.Request{OperationID: tt.opID, Context: context.Background()}
			expectedResp := middleware.Response{Type: "ok"}
			next := fakeNext(&called, expectedResp)
			resp, err := mw(req, next)

			assertGateResult(t, tt.wantCalled, called, tt.wantErrNotNil, tt.wantErrIs, err, resp)
		})
	}
}

func assertGateResult(t *testing.T, wantCalled, called, wantErrNotNil bool, wantErrIs, err error, resp middleware.Response) {
	t.Helper()
	if called != wantCalled {
		t.Fatalf("next called = %v, want %v", called, wantCalled)
	}
	if wantErrNotNil && err == nil {
		t.Fatal("expected error, got nil")
	}
	if !wantErrNotNil && err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wantErrIs != nil && !errors.Is(err, wantErrIs) {
		t.Fatalf("expected error matching %v, got %v", wantErrIs, err)
	}
	if wantErrNotNil && wantErrIs == nil && errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("checker error must not be swallowed as ErrNotFound: %v", err)
	}
	if !wantErrNotNil && resp.Type != "ok" {
		t.Fatalf("expected response type 'ok', got %v", resp.Type)
	}
}

func TestModuleGateToggleSequence(t *testing.T) {
	t.Parallel()
	ops := map[string]string{"testOp": "watchaware"}
	chk := &fakeModuleChecker{enabled: map[string]bool{"watchaware": false}}
	mw := ModuleGateMiddleware(ops, chk)
	req := middleware.Request{OperationID: "testOp", Context: context.Background()}

	var called1 bool
	_, err1 := mw(req, fakeNext(&called1, middleware.Response{Type: 200}))
	if called1 || !errors.Is(err1, domain.ErrNotFound) {
		t.Fatalf("call 1 (off): called=%v, err=%v", called1, err1)
	}

	chk.enabled["watchaware"] = true
	var called2 bool
	resp2, err2 := mw(req, fakeNext(&called2, middleware.Response{Type: 200}))
	if !called2 || err2 != nil || resp2.Type != 200 {
		t.Fatalf("call 2 (on): called=%v, err=%v, resp=%v", called2, err2, resp2)
	}

	chk.enabled["watchaware"] = false
	var called3 bool
	_, err3 := mw(req, fakeNext(&called3, middleware.Response{Type: 200}))
	if called3 || !errors.Is(err3, domain.ErrNotFound) {
		t.Fatalf("call 3 (off): called=%v, err=%v", called3, err3)
	}
}

type testStatusHandler struct {
	oas.UnimplementedHandler
}

func (testStatusHandler) GetSystemStatus(_ context.Context) (*oas.SystemStatus, error) {
	return &oas.SystemStatus{Version: "1.0.0"}, nil
}

func (testStatusHandler) NewError(ctx context.Context, err error) *oas.ProblemStatusCode {
	var h Handlers
	return h.NewError(ctx, err)
}

type testNoopSecurity struct{}

func (testNoopSecurity) HandleBearerAuth(ctx context.Context, _ oas.OperationName, _ oas.BearerAuth) (context.Context, error) {
	return ctx, nil
}

func (testNoopSecurity) HandleCookieAuth(ctx context.Context, _ oas.OperationName, _ oas.CookieAuth) (context.Context, error) {
	return ctx, nil
}

func TestModuleGateOASServerIntegration(t *testing.T) {
	t.Parallel()
	chk := &fakeModuleChecker{enabled: map[string]bool{"watchaware": false}}
	ops := map[string]string{"getSystemStatus": "watchaware"}
	mw := ModuleGateMiddleware(ops, chk)

	srv, err := oas.NewServer(testStatusHandler{}, testNoopSecurity{},
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(slog.New(slog.DiscardHandler))),
		oas.WithMiddleware(mw),
	)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	runOASToggleCases(t, ts.URL+PathPrefix+"/system/status", chk)
}

func runOASToggleCases(t *testing.T, url string, chk *fakeModuleChecker) {
	t.Helper()
	status, cType, body := execHTTPGet(t, url)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", status, body)
	}
	if !strings.Contains(cType, "application/problem+json") {
		t.Fatalf("expected problem+json, got %q", cType)
	}

	chk.enabled["watchaware"] = true
	status, _, body = execHTTPGet(t, url)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", status, body)
	}

	chk.enabled["watchaware"] = false
	status, _, body = execHTTPGet(t, url)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", status, body)
	}
}

func execHTTPGet(t *testing.T, url string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), string(b)
}

func TestModuleOpsRegisteredInRegistry(t *testing.T) {
	t.Parallel()
	reg, err := modules.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	for opID, modID := range moduleops.ByOperation {
		if _, ok := reg.Lookup(modID); !ok {
			t.Errorf("operation %q maps to unregistered module %q", opID, modID)
		}
	}
}
