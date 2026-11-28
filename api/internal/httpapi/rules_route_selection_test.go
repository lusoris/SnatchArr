// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDispatchStreamingRoutesRulesPathNormalization(t *testing.T) {
	t.Parallel()
	const rulesPath = "/library-processing/rules"
	const canonical = PathPrefix + rulesPath

	for _, tt := range []struct {
		name       string
		method     string
		target     string
		rawPath    string
		wantStream bool
	}{
		{name: "canonical", method: http.MethodGet, target: canonical, wantStream: true},
		{name: "escaped unreserved", method: http.MethodGet, target: PathPrefix + "/library-processing/%72ules", wantStream: true},
		{name: "encoded slash", method: http.MethodGet, target: PathPrefix + "/library-processing%2Frules"},
		{name: "wrong method", method: http.MethodPost, target: canonical},
		{name: "missing prefix", method: http.MethodGet, target: rulesPath},
		{name: "different version prefix", method: http.MethodGet, target: PathPrefix + "0" + rulesPath},
		{name: "malformed RawPath falls back to Path", method: http.MethodGet, target: canonical, rawPath: canonical + "%zz", wantStream: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tt.method, tt.target, nil)
			if tt.rawPath != "" {
				req.URL.RawPath = tt.rawPath
			}

			var fallbackCalls, streamCalls int
			fallback := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				fallbackCalls++
			})
			stream := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				streamCalls++
			})
			dispatchStreamingRoutes(fallback, stream).ServeHTTP(httptest.NewRecorder(), req)

			wantFallback, wantStream := 1, 0
			if tt.wantStream {
				wantFallback, wantStream = 0, 1
			}
			if fallbackCalls != wantFallback || streamCalls != wantStream {
				t.Fatalf("handler calls (fallback, stream) = (%d, %d), want (%d, %d)", fallbackCalls, streamCalls, wantFallback, wantStream)
			}
		})
	}
}
