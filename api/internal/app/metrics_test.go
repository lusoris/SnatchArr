// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx/fxtest"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/observability/statuspage"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		addr         string
		expectSep    bool
		wantMainCode int
	}{
		{
			name:         "positive separate listener on free port",
			addr:         freeAddr(t),
			expectSep:    true,
			wantMainCode: http.StatusNotFound,
		},
		{
			name:         "positive empty addr mounts on main router",
			addr:         "",
			expectSep:    false,
			wantMainCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewFake()
			reg := statuspage.NewRegistry(clk)
			lc := fxtest.NewLifecycle(t)

			d := httpDeps{
				LC:     lc,
				Router: chi.NewRouter(),
				Logger: slog.New(slog.DiscardHandler),
				Clock:  clk,
				Reg:    reg,
				Cfg: config.Options{
					Metrics: config.MetricsOptions{
						Addr: tc.addr,
					},
				},
			}

			if err := mountMetrics(d); err != nil {
				t.Fatalf("mountMetrics: %v", err)
			}
			if err := mountWebUI(d); err != nil {
				t.Fatalf("mountWebUI: %v", err)
			}

			lc.RequireStart()
			defer lc.RequireStop()

			if tc.expectSep {
				metricsAddr := tc.addr

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+metricsAddr+"/metrics", nil)
				if err != nil {
					t.Fatalf("new request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("GET /metrics on separate server: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("metrics server status = %d, want 200", resp.StatusCode)
				}
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read metrics body: %v", err)
				}
				bodyStr := string(body)
				if !strings.Contains(bodyStr, "app_check_status") && !strings.Contains(bodyStr, "go_goroutines") {
					t.Fatalf("metrics body missing expected tokens: %s", bodyStr)
				}
			}

			mainReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			mainRec := httptest.NewRecorder()
			d.Router.ServeHTTP(mainRec, mainReq)

			if mainRec.Code != tc.wantMainCode {
				t.Fatalf("main router GET /metrics = %d, want %d", mainRec.Code, tc.wantMainCode)
			}
			if tc.wantMainCode == http.StatusOK {
				bodyStr := mainRec.Body.String()
				if !strings.Contains(bodyStr, "app_check_status") && !strings.Contains(bodyStr, "go_goroutines") {
					t.Fatalf("main router metrics body missing expected tokens: %s", bodyStr)
				}
			}
		})
	}
}
