// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func observe(t *testing.T, kind domain.DownloadClientKind, baseURL, username, secret string) dlclients.Snapshot {
	t.Helper()
	g := dlclients.NewGoenvoy(clock.NewFake())
	c := domain.DownloadClient{Kind: kind, Name: "c", BaseURL: baseURL, Username: username, Enabled: true}
	return g.Snapshot(context.Background(), c, secret)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func rpcMethod(t *testing.T, r *http.Request) string {
	t.Helper()
	var body struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode rpc: %v", err)
	}
	return body.Method
}

func TestQBittorrentSnapshot(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("username") != "admin" || r.FormValue("password") != "pw" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
		_, _ = io.WriteString(w, "Ok.")
	})
	mux.HandleFunc("GET /api/v2/app/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "v5.0.1") })
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"state": "downloading", "dlspeed": 1000, "upspeed": 10},
			{"state": "queuedDL"},
			{"state": "stalledUP", "upspeed": 5},
			{"state": "pausedDL"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := observe(t, domain.ClientQBittorrent, srv.URL, "admin", "pw")
	if !s.Reachable || s.Active != 1 || s.Queued != 1 || s.DownloadRate != 1000 || s.UploadRate != 15 || s.Version != "v5.0.1" {
		t.Fatalf("got %+v", s)
	}
	if bad := observe(t, domain.ClientQBittorrent, srv.URL, "admin", "wrong"); bad.Reachable || bad.Error == "" {
		t.Fatalf("bad credentials must be unreachable: %+v", bad)
	}
}

func TestTransmissionSnapshot(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transmission/rpc", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "tr" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "sess")
			w.WriteHeader(http.StatusConflict)
			return
		}
		switch rpcMethod(t, r) {
		case "session-get":
			writeJSON(t, w, map[string]any{"result": "success", "arguments": map[string]any{"version": "4.0.5"}})
		case "torrent-get":
			writeJSON(t, w, map[string]any{"result": "success", "arguments": map[string]any{"torrents": []map[string]any{
				{"id": 1, "status": 4, "rateDownload": 2000, "rateUpload": 100},
				{"id": 2, "status": 3},
				{"id": 3, "status": 6, "rateUpload": 50},
			}}})
		default:
			writeJSON(t, w, map[string]any{"result": "unknown method"})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, base := range []string{srv.URL, srv.URL + "/transmission", srv.URL + "/transmission/rpc"} {
		s := observe(t, domain.ClientTransmission, base, "tr", "pw")
		if !s.Reachable || s.Active != 1 || s.Queued != 1 || s.DownloadRate != 2000 || s.UploadRate != 150 || s.Version != "4.0.5" {
			t.Fatalf("%s: got %+v", base, s)
		}
	}
}

func TestDelugeSnapshot(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /json", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
			ID     int    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		reply := func(result any) { writeJSON(t, w, map[string]any{"result": result, "error": nil, "id": body.ID}) }
		switch body.Method {
		case "auth.login":
			reply(len(body.Params) == 1 && body.Params[0] == "pw")
		case "daemon.info":
			reply("2.1.1")
		case "core.get_torrents_status":
			reply(map[string]any{
				"a": map[string]any{"state": "Downloading", "download_payload_rate": 300, "upload_payload_rate": 30},
				"b": map[string]any{"state": "Queued"},
				"c": map[string]any{"state": "Seeding", "upload_payload_rate": 20},
			})
		default:
			writeJSON(t, w, map[string]any{"result": nil, "error": map[string]any{"code": 1, "message": "unknown"}, "id": body.ID})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := observe(t, domain.ClientDeluge, srv.URL, "", "pw")
	if !s.Reachable || s.Active != 1 || s.Queued != 1 || s.DownloadRate != 300 || s.UploadRate != 50 || s.Version != "2.1.1" {
		t.Fatalf("got %+v", s)
	}
}

func TestSABnzbdSnapshot(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "key" {
			writeJSON(t, w, map[string]any{"status": false, "error": "API Key Incorrect"})
			return
		}
		switch r.URL.Query().Get("mode") {
		case "version":
			writeJSON(t, w, map[string]any{"version": "4.3.2"})
		case "queue":
			writeJSON(t, w, map[string]any{"queue": map[string]any{
				"paused": true, "paused_all": false, "speed": "1.5 M",
				"slots": []map[string]any{{"status": "Downloading"}, {"status": "Queued"}, {"status": "Queued"}},
			}})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := observe(t, domain.ClientSABnzbd, srv.URL, "", "key")
	if !s.Reachable || !s.Paused || s.Active != 1 || s.Queued != 2 || s.DownloadRate != 1_572_864 || s.Version != "4.3.2" {
		t.Fatalf("got %+v", s)
	}
	if bad := observe(t, domain.ClientSABnzbd, srv.URL, "", "nope"); bad.Reachable {
		t.Fatalf("bad api key must be unreachable: %+v", bad)
	}
}

func TestNZBGetSnapshot(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "nzb" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch rpcMethod(t, r) {
		case "version":
			writeJSON(t, w, map[string]any{"result": "24.3"})
		case "status":
			writeJSON(t, w, map[string]any{"result": map[string]any{"DownloadPaused": false, "DownloadRate": 4096}})
		case "listgroups":
			writeJSON(t, w, map[string]any{"result": []map[string]any{
				{"Status": "DOWNLOADING", "ActiveDownloads": 1}, {"Status": "QUEUED"}, {"Status": "PAUSED"},
			}})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := observe(t, domain.ClientNZBGet, srv.URL, "nzb", "pw")
	if !s.Reachable || s.Paused || s.Active != 1 || s.Queued != 1 || s.DownloadRate != 4096 || s.Version != "24.3" {
		t.Fatalf("got %+v", s)
	}
}

func TestUnreachableClientIsASnapshotNotAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	for _, kind := range domain.AllDownloadClientKinds {
		s := observe(t, kind, url, "u", "p")
		if s.Reachable || s.Error == "" || s.Kind != kind {
			t.Fatalf("%s: got %+v", kind, s)
		}
	}
}

func TestParseSABSpeed(t *testing.T) {
	t.Parallel()
	cases := map[string]int64{
		"1.5 M": 1_572_864, "512 K": 524_288, "2 G": 2_147_483_648, "300": 300, "0 ": 0, "": 0, "abc": 0, "-1 M": 0,
	}
	for in, want := range cases {
		if got := dlclients.ParseSABSpeed(in); got != want {
			t.Errorf("ParseSABSpeed(%q) = %d, want %d", in, got, want)
		}
	}
}
