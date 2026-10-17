// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/logring"
)

// logsPlantedHex is a planted *arr key; the logs endpoints must never serve it.
const logsPlantedHex = "0123456789abcdef0123456789abcdef"

// debugSink is an enabled handler that drops every record: slog.DiscardHandler reports
// every level disabled, so the logger would never call Handle.
type debugSink struct{}

func (debugSink) Enabled(context.Context, slog.Level) bool  { return true }
func (debugSink) Handle(context.Context, slog.Record) error { return nil }
func (s debugSink) WithAttrs([]slog.Attr) slog.Handler      { return s }
func (s debugSink) WithGroup(string) slog.Handler           { return s }

func logsHandlers(t *testing.T) *Handlers {
	t.Helper()
	ring := logring.New(logring.MinSize)
	l := slog.New(ring.Wrap(debugSink{}, logring.SourceAPI))
	l.Debug("cache warm")
	l.Info("planner tick", "instance", "sonarr-main", "api_key", logsPlantedHex)
	l.Warn("budget at 80 %", "indexer", "nzbgeek")
	l.Error("sonarr unreachable", "url", "http://admin:hunter2@sonarr:8989/api/v3?apikey="+logsPlantedHex)
	return &Handlers{logs: ring}
}

func TestListLogs(t *testing.T) {
	t.Parallel()
	admin := withUser(t.Context(), domain.User{Role: domain.RoleAdmin})
	cases := []struct {
		name    string
		ctx     context.Context
		params  oas.ListLogsParams
		want    []string
		wantErr error
	}{
		{
			"positive all levels newest first", admin,
			oas.ListLogsParams{},
			[]string{"sonarr unreachable", "budget at 80 %", "planner tick", "cache warm"},
			nil,
		},
		{
			"positive minimum level warn", admin,
			oas.ListLogsParams{Level: oas.NewOptLogLevel(oas.LogLevelWarn)},
			[]string{"sonarr unreachable", "budget at 80 %"},
			nil,
		},
		{
			"positive text matches an attribute value", admin,
			oas.ListLogsParams{Q: oas.NewOptString("NZBGEEK")},
			[]string{"budget at 80 %"},
			nil,
		},
		{"boundary limit 1", admin, oas.ListLogsParams{Limit: oas.NewOptInt(1)}, []string{"sonarr unreachable"}, nil},
		{"boundary no match", admin, oas.ListLogsParams{Source: oas.NewOptString("worker:w1")}, []string{}, nil},
		{"negative viewer forbidden", withUser(t.Context(), domain.User{Role: domain.RoleViewer}), oas.ListLogsParams{}, nil, domain.ErrForbidden},
		{"negative anonymous", t.Context(), oas.ListLogsParams{}, nil, domain.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page, err := logsHandlers(t).ListLogs(tc.ctx, tc.params)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ListLogs() error = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			got := make([]string, 0, len(page.Records))
			for _, r := range page.Records {
				got = append(got, r.Message)
			}
			if !slices.Equal(got, tc.want) || page.Capacity != logring.MinSize || page.Dropped != 0 {
				t.Fatalf("ListLogs() = %v cap %d dropped %d, want %v cap %d dropped 0", got, page.Capacity, page.Dropped, tc.want, logring.MinSize)
			}
		})
	}
}

// The API never returns a planted secret: the ring redacted it before storage.
func TestListLogsReturnsNoSecret(t *testing.T) {
	t.Parallel()
	page, err := logsHandlers(t).ListLogs(withUser(t.Context(), domain.User{Role: domain.RoleAdmin}), oas.ListLogsParams{})
	if err != nil {
		t.Fatalf("ListLogs() = %v", err)
	}
	for _, r := range page.Records {
		for _, a := range r.Attrs {
			for _, secret := range []string{logsPlantedHex, "hunter2"} {
				if strings.Contains(strings.ToLower(a.Value), secret) {
					t.Errorf("record %q attr %s = %q leaks %q", r.Message, a.Key, a.Value, secret)
				}
			}
		}
	}
	if lvl := page.Records[0].Level; lvl != oas.LogLevelError {
		t.Errorf("newest level = %q, want error", lvl)
	}
}

func TestAdminOnly(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	cases := []struct {
		name string
		user *domain.User
		want int
	}{
		{"positive admin", &domain.User{Role: domain.RoleAdmin}, http.StatusOK},
		{"negative viewer", &domain.User{Role: domain.RoleViewer}, http.StatusForbidden},
		{"negative no user", nil, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, LogStreamPath, nil)
			if tc.user != nil {
				req = req.WithContext(withUser(req.Context(), *tc.user))
			}
			rec := httptest.NewRecorder()
			adminOnly(slog.New(slog.DiscardHandler))(ok).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET %s as %s = %d, want %d", LogStreamPath, tc.name, rec.Code, tc.want)
			}
			if tc.want != http.StatusOK && rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", rec.Header().Get("Content-Type"))
			}
		})
	}
}
