// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_Positive(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		seenAuth    string
		seenPath    string
		seenCT      string
		seenAccept  string
		seenReqBody string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAuth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		seenCT = r.Header.Get("Content-Type")
		seenAccept = r.Header.Get("Accept")
		bodyBytes, _ := io.ReadAll(r.Body)
		seenReqBody = string(bodyBytes)
		mu.Unlock()

		if strings.Contains(seenReqBody, "tools/list") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"test"}]}}` + "\n"))
			return
		}
		if strings.Contains(seenReqBody, "notifications/initialized") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	getenv := func(key string) string {
		switch key {
		case "SNATCHARR_URL":
			return srv.URL
		case "SNATCHARR_API_KEY":
			return "test-bearer-token"
		default:
			return ""
		}
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"

	var stdout, stderr bytes.Buffer
	rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(input), &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("RunWithClient() = %d, want 0; stderr: %q", rc, stderr.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if seenAuth != "Bearer test-bearer-token" {
		t.Errorf("Authorization = %q, want %q", seenAuth, "Bearer test-bearer-token")
	}
	if seenPath != "/api/v1/mcp" {
		t.Errorf("URL path = %q, want %q", seenPath, "/api/v1/mcp")
	}
	if seenCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", seenCT)
	}
	if seenAccept != "application/json, text/event-stream" {
		t.Errorf("Accept = %q, want application/json, text/event-stream", seenAccept)
	}

	outLines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(outLines) != 1 {
		t.Fatalf("got %d stdout lines, want exactly 1; stdout: %q", len(outLines), stdout.String())
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(outLines[0]), &resp); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if resp["id"].(float64) != 1 {
		t.Errorf("resp id = %v, want 1", resp["id"])
	}
}

func TestRun_NegativeHTTP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		status        int
		headers       map[string]string
		respBody      string
		reqLine       string
		wantCode      int
		wantMsg       string
		wantStatus    int
		wantWWWAuth   string
		wantStderr    string
		wantStdoutLen int
	}{
		{
			name:   "403 with insufficient_scope",
			status: http.StatusForbidden,
			headers: map[string]string{
				"Content-Type":     "application/problem+json",
				"WWW-Authenticate": `Bearer error="insufficient_scope", scope="core:read"`,
			},
			respBody:    `{"title":"Forbidden","detail":"key lacks required scope core:read","status":403}`,
			reqLine:     `{"jsonrpc":"2.0","id":"req-403","method":"tools/call","params":{"name":"snatcharr_list_instances"}}`,
			wantCode:    -32000,
			wantMsg:     "snatcharr: HTTP 403: key lacks required scope core:read",
			wantStatus:  403,
			wantWWWAuth: `Bearer error="insufficient_scope", scope="core:read"`,
		},
		{
			name:   "404 module off",
			status: http.StatusNotFound,
			headers: map[string]string{
				"Content-Type": "application/problem+json",
			},
			respBody:   `{"title":"Not Found","detail":"llm module is disabled","status":404}`,
			reqLine:    `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			wantCode:   -32000,
			wantMsg:    "snatcharr: HTTP 404: llm module is disabled",
			wantStatus: 404,
		},
		{
			name:   "401 unauthorized",
			status: http.StatusUnauthorized,
			headers: map[string]string{
				"Content-Type": "application/problem+json",
			},
			respBody:   `{"title":"Unauthorized","detail":"authentication required","status":401}`,
			reqLine:    `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
			wantCode:   -32000,
			wantMsg:    "snatcharr: HTTP 401: authentication required",
			wantStatus: 401,
		},
		{
			name:   "notification 403 writes to stderr only",
			status: http.StatusForbidden,
			headers: map[string]string{
				"Content-Type": "application/problem+json",
			},
			respBody:      `{"title":"Forbidden","detail":"forbidden","status":403}`,
			reqLine:       `{"jsonrpc":"2.0","method":"notifications/update"}`,
			wantStderr:    "snatcharr: HTTP 403: forbidden",
			wantStdoutLen: 0,
		},
		{
			name:   "200 text/event-stream unsupported",
			status: http.StatusOK,
			headers: map[string]string{
				"Content-Type": "text/event-stream",
			},
			respBody: `data: hello\n\n`,
			reqLine:  `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`,
			wantCode: -32000,
			wantMsg:  "snatcharr: streaming text/event-stream response is unsupported",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.respBody))
			}))
			t.Cleanup(srv.Close)

			getenv := func(key string) string {
				if key == "SNATCHARR_URL" {
					return srv.URL
				}
				if key == "SNATCHARR_API_KEY" {
					return "test-key"
				}
				return ""
			}

			var stdout, stderr bytes.Buffer
			rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(tc.reqLine+"\n"), &stdout, &stderr)
			if rc != 0 {
				t.Fatalf("RunWithClient() = %d, want 0; stderr: %q", rc, stderr.String())
			}

			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tc.wantStderr)
			}
			if tc.wantStdoutLen == 0 && tc.wantStderr != "" {
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want empty", stdout.String())
				}
				return
			}

			var errResp struct {
				JSONRPC string `json:"jsonrpc"`
				ID      any    `json:"id"`
				Error   struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Data    struct {
						Status          int    `json:"status"`
						WWWAuthenticate string `json:"www_authenticate"`
					} `json:"data"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
				t.Fatalf("stdout is not valid JSON-RPC error: %v; stdout: %q", err, stdout.String())
			}
			if errResp.Error.Code != tc.wantCode {
				t.Errorf("error.code = %d, want %d", errResp.Error.Code, tc.wantCode)
			}
			if !strings.Contains(errResp.Error.Message, tc.wantMsg) {
				t.Errorf("error.message = %q, want containing %q", errResp.Error.Message, tc.wantMsg)
			}
			if tc.wantStatus != 0 && errResp.Error.Data.Status != tc.wantStatus {
				t.Errorf("error.data.status = %d, want %d", errResp.Error.Data.Status, tc.wantStatus)
			}
			if tc.wantWWWAuth != "" && errResp.Error.Data.WWWAuthenticate != tc.wantWWWAuth {
				t.Errorf("error.data.www_authenticate = %q, want %q", errResp.Error.Data.WWWAuthenticate, tc.wantWWWAuth)
			}
		})
	}
}

func TestRun_NegativeValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		args       []string
		url        string
		key        string
		wantRC     int
		wantStderr string
	}{
		{
			name:       "unknown subcommand",
			args:       []string{"other"},
			url:        "http://localhost:8080",
			key:        "secret",
			wantRC:     2,
			wantStderr: "Usage:",
		},
		{
			name:       "no subcommand",
			args:       []string{},
			url:        "http://localhost:8080",
			key:        "secret",
			wantRC:     2,
			wantStderr: "Usage:",
		},
		{
			name:       "missing SNATCHARR_URL",
			args:       []string{"stdio"},
			url:        "",
			key:        "secret",
			wantRC:     2,
			wantStderr: "SNATCHARR_URL is required",
		},
		{
			name:       "bad scheme ftp",
			args:       []string{"stdio"},
			url:        "ftp://localhost:8080",
			key:        "secret",
			wantRC:     2,
			wantStderr: "SNATCHARR_URL must be an http or https URL with host",
		},
		{
			name:       "bad scheme no host",
			args:       []string{"stdio"},
			url:        "not-a-url",
			key:        "secret",
			wantRC:     2,
			wantStderr: "SNATCHARR_URL must be an http or https URL with host",
		},
		{
			name:       "missing SNATCHARR_API_KEY",
			args:       []string{"stdio"},
			url:        "http://localhost:8080",
			key:        "",
			wantRC:     2,
			wantStderr: "SNATCHARR_API_KEY is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key == "SNATCHARR_URL" {
					return tc.url
				}
				if key == "SNATCHARR_API_KEY" {
					return tc.key
				}
				return ""
			}
			var stdout, stderr bytes.Buffer
			rc := Run(t.Context(), tc.args, getenv, strings.NewReader(""), &stdout, &stderr)
			if rc != tc.wantRC {
				t.Fatalf("Run() = %d, want %d", rc, tc.wantRC)
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

func TestRun_KeyNeverPrinted(t *testing.T) {
	t.Parallel()

	canary := "canary-super-secret-key-xyz123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","detail":"bad key","status":401}`))
	}))
	t.Cleanup(srv.Close)

	getenv := func(key string) string {
		if key == "SNATCHARR_URL" {
			return srv.URL
		}
		if key == "SNATCHARR_API_KEY" {
			return canary
		}
		return ""
	}

	var stdout, stderr bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	_ = RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(input), &stdout, &stderr)

	if strings.Contains(stdout.String(), canary) {
		t.Errorf("canary key leaked in stdout: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), canary) {
		t.Errorf("canary key leaked in stderr: %q", stderr.String())
	}
}

// TestRun_Boundary covers the edges of the stdio bridge: a line of exactly
// 1 MiB is forwarded, one byte more is refused with -32600, empty lines are
// skipped, and a request that outlives the HTTP client timeout returns a
// -32000 error carrying the request id, also when the run starts late.
func TestRun_Boundary(t *testing.T) {
	t.Parallel()

	t.Run("line exactly 1 MiB passes to server", func(t *testing.T) {
		t.Parallel()
		var receivedLen int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			receivedLen = len(body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":100,"result":"ok"}` + "\n"))
		}))
		t.Cleanup(srv.Close)

		getenv := func(key string) string {
			if key == "SNATCHARR_URL" {
				return srv.URL
			}
			if key == "SNATCHARR_API_KEY" {
				return "test-key"
			}
			return ""
		}

		prefix := `{"jsonrpc":"2.0","id":100,"method":"test","params":"`
		suffix := `"}`
		padLen := maxLineBytes - len(prefix) - len(suffix)
		reqLine := prefix + strings.Repeat("a", padLen) + suffix
		if len(reqLine) != maxLineBytes {
			t.Fatalf("reqLine len = %d, want %d", len(reqLine), maxLineBytes)
		}

		var stdout, stderr bytes.Buffer
		rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(reqLine+"\n"), &stdout, &stderr)
		if rc != 0 {
			t.Fatalf("RunWithClient() = %d, want 0; stderr: %q", rc, stderr.String())
		}
		if receivedLen != maxLineBytes {
			t.Errorf("server received body len = %d, want %d", receivedLen, maxLineBytes)
		}
		if !strings.Contains(stdout.String(), `"id":100`) {
			t.Errorf("stdout %q does not contain id:100", stdout.String())
		}
	})

	t.Run("line 1 MiB + 1 returns -32600", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		getenv := func(key string) string {
			if key == "SNATCHARR_URL" {
				return srv.URL
			}
			if key == "SNATCHARR_API_KEY" {
				return "test-key"
			}
			return ""
		}

		prefix := `{"jsonrpc":"2.0","id":200,"method":"test","params":"`
		suffix := `"}`
		padLen := maxLineBytes + 1 - len(prefix) - len(suffix)
		reqLine := prefix + strings.Repeat("b", padLen) + suffix
		if len(reqLine) != maxLineBytes+1 {
			t.Fatalf("reqLine len = %d, want %d", len(reqLine), maxLineBytes+1)
		}

		var stdout, stderr bytes.Buffer
		rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(reqLine+"\n"), &stdout, &stderr)
		if rc != 0 {
			t.Fatalf("RunWithClient() = %d, want 0; stderr: %q", rc, stderr.String())
		}
		var errResp struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Error   struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
			t.Fatalf("stdout is not valid JSON: %v; stdout: %q", err, stdout.String())
		}
		if errResp.Error.Code != -32600 {
			t.Errorf("error.code = %d, want -32600", errResp.Error.Code)
		}
		if errResp.ID != 200 {
			t.Errorf("id = %d, want 200", errResp.ID)
		}
		if !strings.Contains(errResp.Error.Message, "message exceeds 1 MiB") {
			t.Errorf("error.message = %q, want 'message exceeds 1 MiB'", errResp.Error.Message)
		}
	})

	t.Run("empty lines skipped and EOF exits 0", func(t *testing.T) {
		t.Parallel()
		var count int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			count++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}` + "\n"))
		}))
		t.Cleanup(srv.Close)

		getenv := func(key string) string {
			if key == "SNATCHARR_URL" {
				return srv.URL
			}
			if key == "SNATCHARR_API_KEY" {
				return "test-key"
			}
			return ""
		}

		input := "\n\n  \n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n\n"
		var stdout, stderr bytes.Buffer
		rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(input), &stdout, &stderr)
		if rc != 0 {
			t.Fatalf("RunWithClient() = %d, want 0", rc)
		}
		if count != 1 {
			t.Errorf("server handled %d requests, want exactly 1", count)
		}
	})

	// The request, not the whole run, carries the 50ms deadline: a run-wide
	// deadline that short can expire before stdin is read on a loaded host.
	t.Run("server timeout returns error with id", func(t *testing.T) {
		t.Parallel()
		assertRequestTimeout(t, 0)
	})

	t.Run("server timeout survives slow start", func(t *testing.T) {
		t.Parallel()
		assertRequestTimeout(t, 200*time.Millisecond)
	})
}

const timeoutInput = `{"jsonrpc":"2.0","id":"timeout-id","method":"slow"}` + "\n"

// assertRequestTimeout runs one request against a server that never answers
// in time and checks the JSON-RPC error keeps the request id. startDelay
// passes between creating the run context and starting the run, standing in
// for a host too loaded to schedule the run promptly.
func assertRequestTimeout(t *testing.T, startDelay time.Duration) {
	t.Helper()
	release := make(chan struct{})
	var reached atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close never waits on the handler

	getenv := func(key string) string {
		switch key {
		case "SNATCHARR_URL":
			return srv.URL
		case "SNATCHARR_API_KEY":
			return "test-key"
		}
		return ""
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client := &http.Client{Transport: srv.Client().Transport, Timeout: 50 * time.Millisecond}

	time.Sleep(startDelay)
	var stdout, stderr bytes.Buffer
	_ = RunWithClient(ctx, client, []string{"stdio"}, getenv, strings.NewReader(timeoutInput), &stdout, &stderr)

	if !reached.Load() {
		t.Error("request never reached the test server, so the error is not a timeout")
	}
	if !strings.Contains(stdout.String(), `"timeout-id"`) {
		t.Errorf("stdout %q does not contain timeout-id", stdout.String())
	}
	if !strings.Contains(stdout.String(), `code":-32000`) {
		t.Errorf("stdout %q does not contain code -32000", stdout.String())
	}
}

func TestRun_OKResponseInvalidJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		respBody   string
		reqLine    string
		hasID      bool
		wantID     string
		wantCode   int
		wantMsg    string
		wantStatus int
		wantStderr string
	}{
		{
			name:       "multiline non-json 200 body with id yields exactly one error line",
			respBody:   "first line\nsecond line\nthird line\n",
			reqLine:    `{"jsonrpc":"2.0","id":"multiline-id","method":"test"}`,
			hasID:      true,
			wantID:     "multiline-id",
			wantCode:   -32000,
			wantMsg:    "snatcharr: invalid JSON response from server",
			wantStatus: 200,
		},
		{
			name:       "empty 200 body with id yields exactly one error line",
			respBody:   "",
			reqLine:    `{"jsonrpc":"2.0","id":"empty-id","method":"test"}`,
			hasID:      true,
			wantID:     "empty-id",
			wantCode:   -32000,
			wantMsg:    "snatcharr: invalid JSON response from server",
			wantStatus: 200,
		},
		{
			name:       "whitespace-only 200 body with id yields exactly one error line",
			respBody:   "   \n\t  \n",
			reqLine:    `{"jsonrpc":"2.0","id":"ws-id","method":"test"}`,
			hasID:      true,
			wantID:     "ws-id",
			wantCode:   -32000,
			wantMsg:    "snatcharr: invalid JSON response from server",
			wantStatus: 200,
		},
		{
			name:       "multiline non-json 200 body notification yields stderr only",
			respBody:   "first line\nsecond line\n",
			reqLine:    `{"jsonrpc":"2.0","method":"notify"}`,
			hasID:      false,
			wantStderr: "snatcharr: invalid JSON response from server",
		},
		{
			name:       "empty 200 body notification yields stderr only",
			respBody:   "",
			reqLine:    `{"jsonrpc":"2.0","method":"notify"}`,
			hasID:      false,
			wantStderr: "snatcharr: invalid JSON response from server",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.respBody))
			}))
			t.Cleanup(srv.Close)

			getenv := func(key string) string {
				if key == "SNATCHARR_URL" {
					return srv.URL
				}
				if key == "SNATCHARR_API_KEY" {
					return "test-key"
				}
				return ""
			}

			var stdout, stderr bytes.Buffer
			rc := RunWithClient(t.Context(), srv.Client(), []string{"stdio"}, getenv, strings.NewReader(tc.reqLine+"\n"), &stdout, &stderr)
			if rc != 0 {
				t.Fatalf("RunWithClient() = %d, want 0; stderr: %q", rc, stderr.String())
			}

			if !tc.hasID {
				if stdout.Len() != 0 {
					t.Fatalf("stdout = %q, want empty", stdout.String())
				}
				if !strings.Contains(stderr.String(), tc.wantStderr) {
					t.Fatalf("stderr %q does not contain %q", stderr.String(), tc.wantStderr)
				}
				return
			}

			outLines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
			if len(outLines) != 1 {
				t.Fatalf("got %d stdout lines, want exactly 1; stdout: %q", len(outLines), stdout.String())
			}

			var errResp struct {
				JSONRPC string `json:"jsonrpc"`
				ID      string `json:"id"`
				Error   struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Data    struct {
						Status int `json:"status"`
					} `json:"data"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(outLines[0]), &errResp); err != nil {
				t.Fatalf("stdout line is not valid JSON: %v; line: %q", err, outLines[0])
			}
			if errResp.ID != tc.wantID {
				t.Errorf("resp id = %q, want %q", errResp.ID, tc.wantID)
			}
			if errResp.Error.Code != tc.wantCode {
				t.Errorf("error.code = %d, want %d", errResp.Error.Code, tc.wantCode)
			}
			if errResp.Error.Message != tc.wantMsg {
				t.Errorf("error.message = %q, want %q", errResp.Error.Message, tc.wantMsg)
			}
			if errResp.Error.Data.Status != tc.wantStatus {
				t.Errorf("error.data.status = %d, want %d", errResp.Error.Data.Status, tc.wantStatus)
			}
		})
	}
}
