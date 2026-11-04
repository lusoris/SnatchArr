// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const (
	testHash40a = "0123456789abcdef0123456789abcdef01234567"
	testHash40b = "abcdef0123456789abcdef0123456789abcdef01"
	testHash64  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type writerRecordedReq struct {
	Method  string
	Path    string
	Query   url.Values
	Form    url.Values
	JSON    map[string]any
	RawBody string
}

type writerFakeServer struct {
	mu          sync.Mutex
	requests    []writerRecordedReq
	failStatus  int
	falseResult bool
	failOnItem  string
}

// record copies the method, path, query and body of r for later assertions.
func (s *writerFakeServer) record(r *http.Request) writerRecordedReq {
	rec := writerRecordedReq{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
	}
	body, _ := io.ReadAll(r.Body)
	rec.RawBody = string(body)
	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		vals, _ := url.ParseQuery(rec.RawBody)
		rec.Form = vals
	} else if len(body) > 0 {
		var j map[string]any
		if err := json.Unmarshal(body, &j); err == nil {
			rec.JSON = j
		}
	}
	return rec
}

// serveHTTP records every request and answers it by request endpoint: /jsonrpc as NZBGet, /api
// as SABnzbd, everything else as qBittorrent.
func (s *writerFakeServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	req := s.record(r)
	s.requests = append(s.requests, req)
	fail := s.failStatus
	falseRes := s.falseResult
	failItem := s.failOnItem
	s.mu.Unlock()

	if fail > 0 {
		http.Error(w, "server error", fail)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/jsonrpc":
		s.serveNZBGet(w, req, falseRes)
	case "/api":
		if failItem != "" && req.Query.Get("value") == failItem {
			http.Error(w, "item error", http.StatusInternalServerError)
			return
		}
		s.serveSABnzbd(w, req, falseRes)
	case "/api/v2/auth/login":
		fmt.Fprint(w, "Ok.")
	default:
		// qBittorrent endpoints.
		fmt.Fprint(w, "Ok.")
	}
}

// serveNZBGet answers a JSON-RPC call with true, or false when falseRes is set.
func (s *writerFakeServer) serveNZBGet(w http.ResponseWriter, req writerRecordedReq, falseRes bool) {
	id := 1
	if req.JSON != nil {
		if rawID, ok := req.JSON["id"].(float64); ok {
			id = int(rawID)
		}
	}
	result := !falseRes
	resp := map[string]any{"version": "2.0", "result": result, "id": id}
	_ = json.NewEncoder(w).Encode(resp)
}

// serveSABnzbd answers a SABnzbd priority call with position 1, or -1 when falseRes is set, and
// any other call with status true, or false when falseRes is set.
func (s *writerFakeServer) serveSABnzbd(w http.ResponseWriter, req writerRecordedReq, falseRes bool) {
	name := req.Query.Get("name")
	if name == "priority" {
		pos := 1
		if falseRes {
			pos = -1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"position": pos})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": !falseRes})
}

// getRequests returns a copy of the recorded requests.
func (s *writerFakeServer) getRequests() []writerRecordedReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// setupWriterTest starts a fake client server and returns it with a writer and a client row aimed at it.
func setupWriterTest(t *testing.T) (*writerFakeServer, *Goenvoy, domain.DownloadClient) {
	t.Helper()
	fake := &writerFakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(srv.Close)

	g := &Goenvoy{
		timeout:   5 * time.Second,
		userAgent: "SnatchArr/test",
		clk:       clock.NewFake(),
	}
	client := domain.DownloadClient{
		BaseURL: srv.URL,
	}
	return fake, g, client
}

// TestPriorityWriter_Positive checks that each allow-listed write sends exactly the expected request.
func TestPriorityWriter_Positive(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name       string
		kind       domain.DownloadClientKind
		call       func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error
		wantPath   string
		wantMethod string
		verify     func(t *testing.T, req writerRecordedReq)
	}

	tests := []testCase{
		{
			name: "qbit top priority",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a, testHash40b})
			},
			wantPath:   "/api/v2/torrents/topPrio",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				wantHashes := testHash40a + "|" + testHash40b
				if got := req.Form.Get("hashes"); got != wantHashes {
					t.Errorf("hashes form = %q, want %q", got, wantHashes)
				}
			},
		},
		{
			name: "qbit bottom priority",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToBottom(ctx, c, "", []string{testHash40a, testHash40b})
			},
			wantPath:   "/api/v2/torrents/bottomPrio",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				wantHashes := testHash40a + "|" + testHash40b
				if got := req.Form.Get("hashes"); got != wantHashes {
					t.Errorf("hashes form = %q, want %q", got, wantHashes)
				}
			},
		},
		{
			name: "qbit pause items (stop torrents)",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{testHash40a, testHash40b})
			},
			wantPath:   "/api/v2/torrents/stop",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				wantHashes := testHash40a + "|" + testHash40b
				if got := req.Form.Get("hashes"); got != wantHashes {
					t.Errorf("hashes form = %q, want %q", got, wantHashes)
				}
			},
		},
		{
			name: "qbit resume items (start torrents)",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.ResumeItems(ctx, c, "", []string{testHash40a, testHash40b})
			},
			wantPath:   "/api/v2/torrents/start",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				wantHashes := testHash40a + "|" + testHash40b
				if got := req.Form.Get("hashes"); got != wantHashes {
					t.Errorf("hashes form = %q, want %q", got, wantHashes)
				}
			},
		},
		{
			name: "sabnzbd set priority",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "nzo1", 0)
			},
			wantPath:   "/api",
			wantMethod: http.MethodGet,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				q := req.Query
				if q.Get("mode") != "queue" || q.Get("name") != "priority" || q.Get("value") != "nzo1" || q.Get("value2") != "0" {
					t.Errorf("unexpected SABnzbd priority query: %v", q)
				}
			},
		},
		{
			name: "sabnzbd pause item",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"nzo1"})
			},
			wantPath:   "/api",
			wantMethod: http.MethodGet,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				q := req.Query
				if q.Get("mode") != "queue" || q.Get("name") != "pause" || q.Get("value") != "nzo1" {
					t.Errorf("unexpected SABnzbd pause query: %v", q)
				}
			},
		},
		{
			name: "sabnzbd resume item",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.ResumeItems(ctx, c, "", []string{"nzo1"})
			},
			wantPath:   "/api",
			wantMethod: http.MethodGet,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				q := req.Query
				if q.Get("mode") != "queue" || q.Get("name") != "resume" || q.Get("value") != "nzo1" {
					t.Errorf("unexpected SABnzbd resume query: %v", q)
				}
			},
		},
		{
			name: "nzbget set priority",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "10", 50)
			},
			wantPath:   "/jsonrpc",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				if req.JSON["method"] != "editqueue" {
					t.Fatalf("method = %v, want editqueue", req.JSON["method"])
				}
				params, ok := req.JSON["params"].([]any)
				if !ok || len(params) != 3 {
					t.Fatalf("unexpected params shape: %v", req.JSON["params"])
				}
				if params[0] != "GroupSetPriority" || params[1] != "50" {
					t.Errorf("unexpected command/param: %v, %v", params[0], params[1])
				}
				ids, ok := params[2].([]any)
				if !ok || !slices.Equal(ids, []any{float64(10)}) {
					t.Errorf("unexpected item IDs param: %v", params[2])
				}
			},
		},
		{
			name: "nzbget pause items",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"10", "20"})
			},
			wantPath:   "/jsonrpc",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				if req.JSON["method"] != "editqueue" {
					t.Fatalf("method = %v, want editqueue", req.JSON["method"])
				}
				params, ok := req.JSON["params"].([]any)
				if !ok || len(params) != 3 {
					t.Fatalf("unexpected params shape: %v", req.JSON["params"])
				}
				if params[0] != "GroupPause" || params[1] != "" {
					t.Errorf("unexpected command/param: %v, %v", params[0], params[1])
				}
				ids, ok := params[2].([]any)
				if !ok || !slices.Equal(ids, []any{float64(10), float64(20)}) {
					t.Errorf("unexpected item IDs param: %v", params[2])
				}
			},
		},
		{
			name: "nzbget resume items",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.ResumeItems(ctx, c, "", []string{"10", "20"})
			},
			wantPath:   "/jsonrpc",
			wantMethod: http.MethodPost,
			verify: func(t *testing.T, req writerRecordedReq) {
				t.Helper()
				if req.JSON["method"] != "editqueue" {
					t.Fatalf("method = %v, want editqueue", req.JSON["method"])
				}
				params, ok := req.JSON["params"].([]any)
				if !ok || len(params) != 3 {
					t.Fatalf("unexpected params shape: %v", req.JSON["params"])
				}
				if params[0] != "GroupResume" || params[1] != "" {
					t.Errorf("unexpected command/param: %v, %v", params[0], params[1])
				}
				ids, ok := params[2].([]any)
				if !ok || !slices.Equal(ids, []any{float64(10), float64(20)}) {
					t.Errorf("unexpected item IDs param: %v", params[2])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake, g, client := setupWriterTest(t)
			client.Kind = tt.kind

			ctx := t.Context()
			if err := tt.call(ctx, g, client); err != nil {
				t.Fatalf("unexpected call error: %v", err)
			}

			reqs := fake.getRequests()
			if len(reqs) != 1 {
				t.Fatalf("recorded %d requests, want 1", len(reqs))
			}
			req := reqs[0]
			if req.Method != tt.wantMethod {
				t.Errorf("method = %q, want %q", req.Method, tt.wantMethod)
			}
			if req.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", req.Path, tt.wantPath)
			}
			tt.verify(t, req)
		})
	}
}

// TestPriorityWriter_Negative checks that invalid IDs, levels and kinds fail before any request,
// and that a server 500 or a false result fails after its one request.
func TestPriorityWriter_Negative(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name       string
		kind       domain.DownloadClientKind
		failStatus int
		falseRes   bool
		call       func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error
		wantErr    error
	}

	tests := []testCase{
		{
			name: "empty item ID in SetItemPriority",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "whitespace item ID in SetItemPriority nzbget",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "   ", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "tab item ID in SetItemPriority nzbget",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "\t", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget 0 item ID in SetItemPriority",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "0", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget negative item ID in SetItemPriority",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "-1", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget 0 item ID in PauseItems",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"0"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget negative item ID in ResumeItems",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.ResumeItems(ctx, c, "", []string{"-1"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget whitespace item ID in PauseItems",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{" "})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget tab item ID in PauseItems",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"\t"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "whitespace item ID in SetItemPriority sabnzbd",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", " ", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "tab item ID in SetItemPriority sabnzbd",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "\t", 0)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "sabnzbd whitespace item ID in PauseItems",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{" "})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "sabnzbd tab item ID in PauseItems",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"\t"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "sabnzbd space within item ID in PauseItems",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"a b"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "sabnzbd control char in item ID in PauseItems",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{"a\x00b"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit all rejected in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{"all"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit pipe rejected in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{"a|b"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit space rejected in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{" "})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit tab rejected in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{"\t"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit 39 hex refused",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{strings.Repeat("a", 39)})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit 41 hex refused",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{strings.Repeat("a", 41)})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "qbit case insensitive duplicate hash rejected",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a, strings.ToUpper(testHash40a)})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "empty IDs in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", nil)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "empty IDs in PauseItems",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "duplicate IDs in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a, testHash40a})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "duplicate IDs in ResumeItems",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.ResumeItems(ctx, c, "", []string{"1", "2", "1"})
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "nzbget force level refused",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "1", 900)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "sabnzbd force level refused",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "nzo1", 2)
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "transmission unsupported for SetItemPriority",
			kind: domain.ClientTransmission,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "id1", 0)
			},
			wantErr: ErrPriorityUnsupported,
		},
		{
			name: "deluge unsupported for MoveToTop",
			kind: domain.ClientDeluge,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a})
			},
			wantErr: ErrPriorityUnsupported,
		},
		{
			name: "rtorrent unsupported for PauseItems",
			kind: domain.ClientRTorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{testHash40a})
			},
			wantErr: ErrPriorityUnsupported,
		},
		{
			name: "qbit unsupported for SetItemPriority",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", testHash40a, 0)
			},
			wantErr: ErrPriorityUnsupported,
		},
		{
			name:       "server 500 error",
			kind:       domain.ClientQBittorrent,
			failStatus: http.StatusInternalServerError,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a})
			},
		},
		{
			name:     "nzbget false result error",
			kind:     domain.ClientNZBGet,
			falseRes: true,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "1", 100)
			},
		},
		{
			name:     "sabnzbd false result error",
			kind:     domain.ClientSABnzbd,
			falseRes: true,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "nzo1", 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake, g, client := setupWriterTest(t)
			client.Kind = tt.kind
			fake.failStatus = tt.failStatus
			fake.falseResult = tt.falseRes

			ctx := t.Context()
			err := tt.call(ctx, g, client)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("error %v does not wrap %v", err, tt.wantErr)
			}
			if tt.failStatus == 0 && !tt.falseRes && len(fake.getRequests()) != 0 {
				t.Errorf("recorded %d requests on pre-network error, want 0", len(fake.getRequests()))
			}
		})
	}
}

// TestPriorityWriter_Boundary checks the edges of the accepted ID and level ranges.
func TestPriorityWriter_Boundary(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name       string
		kind       domain.DownloadClientKind
		call       func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error
		wantReqCnt int
	}

	nzbBest := domain.NZBGetPriorityLevels().Best2Worst[0]
	nzbWorst := domain.NZBGetPriorityLevels().Best2Worst[len(domain.NZBGetPriorityLevels().Best2Worst)-1]
	sabBest := domain.SABnzbdPriorityLevels().Best2Worst[0]
	sabWorst := domain.SABnzbdPriorityLevels().Best2Worst[len(domain.SABnzbdPriorityLevels().Best2Worst)-1]

	tests := []testCase{
		{
			name: "single 40-hex id in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash40a})
			},
			wantReqCnt: 1,
		},
		{
			name: "single 64-hex id in MoveToTop",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToTop(ctx, c, "", []string{testHash64})
			},
			wantReqCnt: 1,
		},
		{
			name: "single id in MoveToBottom",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.MoveToBottom(ctx, c, "", []string{testHash40a})
			},
			wantReqCnt: 1,
		},
		{
			name: "single id in PauseItems",
			kind: domain.ClientQBittorrent,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.PauseItems(ctx, c, "", []string{testHash40a})
			},
			wantReqCnt: 1,
		},
		{
			name: "nzbget best level boundary",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "1", nzbBest)
			},
			wantReqCnt: 1,
		},
		{
			name: "nzbget worst level boundary",
			kind: domain.ClientNZBGet,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "1", nzbWorst)
			},
			wantReqCnt: 1,
		},
		{
			name: "sabnzbd best level boundary",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "nzo1", sabBest)
			},
			wantReqCnt: 1,
		},
		{
			name: "sabnzbd worst level boundary",
			kind: domain.ClientSABnzbd,
			call: func(ctx context.Context, g *Goenvoy, c domain.DownloadClient) error {
				return g.SetItemPriority(ctx, c, "", "nzo1", sabWorst)
			},
			wantReqCnt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake, g, client := setupWriterTest(t)
			client.Kind = tt.kind

			ctx := t.Context()
			if err := tt.call(ctx, g, client); err != nil {
				t.Fatalf("unexpected call error: %v", err)
			}
			reqs := fake.getRequests()
			if len(reqs) != tt.wantReqCnt {
				t.Errorf("recorded %d requests, want %d", len(reqs), tt.wantReqCnt)
			}
		})
	}
}

// TestPriorityWriter_PartialWrite checks that a failed later SABnzbd request reports the completed items.
func TestPriorityWriter_PartialWrite(t *testing.T) {
	t.Parallel()

	t.Run("sabnzbd pause second request fails", func(t *testing.T) {
		t.Parallel()
		fake, g, client := setupWriterTest(t)
		client.Kind = domain.ClientSABnzbd
		fake.failOnItem = "nzo2"

		ctx := t.Context()
		err := g.PauseItems(ctx, client, "", []string{"nzo1", "nzo2"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var partialErr *PartialWriteError
		if !errors.As(err, &partialErr) {
			t.Fatalf("expected *PartialWriteError, got %T: %v", err, err)
		}
		if len(partialErr.Done) != 1 || partialErr.Done[0] != "nzo1" {
			t.Errorf("Done = %v, want ['nzo1']", partialErr.Done)
		}
		if partialErr.Unwrap() == nil {
			t.Error("expected non-nil unwrapped error")
		}
		if len(fake.getRequests()) != 2 {
			t.Errorf("recorded %d requests, want 2", len(fake.getRequests()))
		}
	})

	t.Run("sabnzbd resume second request fails", func(t *testing.T) {
		t.Parallel()
		fake, g, client := setupWriterTest(t)
		client.Kind = domain.ClientSABnzbd
		fake.failOnItem = "nzo2"

		ctx := t.Context()
		err := g.ResumeItems(ctx, client, "", []string{"nzo1", "nzo2"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var partialErr *PartialWriteError
		if !errors.As(err, &partialErr) {
			t.Fatalf("expected *PartialWriteError, got %T: %v", err, err)
		}
		if len(partialErr.Done) != 1 || partialErr.Done[0] != "nzo1" {
			t.Errorf("Done = %v, want ['nzo1']", partialErr.Done)
		}
		if partialErr.Unwrap() == nil {
			t.Error("expected non-nil unwrapped error")
		}
		if len(fake.getRequests()) != 2 {
			t.Errorf("recorded %d requests, want 2", len(fake.getRequests()))
		}
	})
}
