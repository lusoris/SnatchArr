// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golusoris/goenvoy/downloadclient/deluge/v2"
	"github.com/golusoris/goenvoy/downloadclient/rtorrent/v2"
	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"
	"github.com/golusoris/goenvoy/downloadclient/transmission/v2"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestQBittorrent_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state string
		want  dlclients.DLClass
	}{
		// Active
		{"downloading", dlclients.DLClassActive},
		{"forcedDL", dlclients.DLClassActive},
		// Queued
		{"stalledDL", dlclients.DLClassQueued},
		{"metaDL", dlclients.DLClassQueued},
		{"forcedMetaDL", dlclients.DLClassQueued},
		{"checkingDL", dlclients.DLClassQueued},
		{"checkingResumeData", dlclients.DLClassQueued},
		{"queuedDL", dlclients.DLClassQueued},
		{"allocating", dlclients.DLClassQueued},
		// None
		{"stoppedDL", dlclients.DLClassNone},
		{"pausedDL", dlclients.DLClassNone},
		{"moving", dlclients.DLClassNone},
		{"uploading", dlclients.DLClassNone},
		{"stalledUP", dlclients.DLClassNone},
		{"queuedUP", dlclients.DLClassNone},
		{"forcedUP", dlclients.DLClassNone},
		{"checkingUP", dlclients.DLClassNone},
		{"stoppedUP", dlclients.DLClassNone},
		{"pausedUP", dlclients.DLClassNone},
		{"error", dlclients.DLClassNone},
		{"missingFiles", dlclients.DLClassNone},
		{"unknown", dlclients.DLClassNone},
		// Negative: unknown string and case sensitivity
		{"Downloading", dlclients.DLClassNone},
		{"completelyUnknown", dlclients.DLClassNone},
		{"", dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifyQbit(tc.state); got != tc.want {
				t.Errorf("ClassifyQbit(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func qbitTestServer(torrents []map[string]any) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "v5.3.0")
	})
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(torrents)
	})
	return httptest.NewServer(mux)
}

func TestQBittorrent_DocumentedStatesHTTP(t *testing.T) {
	t.Parallel()
	allStates := []map[string]any{
		{"state": "downloading", "dlspeed": 100},
		{"state": "forcedDL", "dlspeed": 50},
		{"state": "stalledDL"},
		{"state": "metaDL"},
		{"state": "forcedMetaDL"},
		{"state": "checkingDL"},
		{"state": "checkingResumeData"},
		{"state": "queuedDL"},
		{"state": "allocating"},
		{"state": "stoppedDL"},
		{"state": "pausedDL"},
		{"state": "moving"},
		{"state": "uploading"},
		{"state": "stalledUP"},
		{"state": "queuedUP"},
		{"state": "forcedUP"},
		{"state": "checkingUP"},
		{"state": "stoppedUP"},
		{"state": "pausedUP"},
		{"state": "error"},
		{"state": "missingFiles"},
		{"state": "unknown"},
		{"state": "unexpectedCustom"},
	}
	cases := []struct {
		name       string
		torrents   []map[string]any
		wantActive int
		wantQueued int
	}{
		{"all documented states", allStates, 2, 7},
		{"boundary: empty list", []map[string]any{}, 0, 0},
		{"boundary: single downloading", []map[string]any{{"state": "downloading"}}, 1, 0},
		{"boundary: all stopped/paused", []map[string]any{{"state": "stoppedDL"}, {"state": "pausedDL"}}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := qbitTestServer(tc.torrents)
			defer srv.Close()
			s := observe(t, domain.ClientQBittorrent, srv.URL, "", "")
			if !s.Reachable || s.Active != tc.wantActive || s.Queued != tc.wantQueued {
				t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=%d, queued=%d",
					s.Reachable, s.Active, s.Queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}

func TestTransmission_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		t    transmission.Torrent
		want dlclients.DLClass
	}{
		{"status 4 active", transmission.Torrent{Status: 4}, dlclients.DLClassActive},
		{"status 4 stalled", transmission.Torrent{Status: 4, IsStalled: true}, dlclients.DLClassQueued},
		{"status 4 errored", transmission.Torrent{Status: 4, Error: 1}, dlclients.DLClassNone},
		{"status 3 queued", transmission.Torrent{Status: 3}, dlclients.DLClassQueued},
		{"status 3 errored", transmission.Torrent{Status: 3, Error: 1}, dlclients.DLClassNone},
		{"status 1 check wait unfinished", transmission.Torrent{Status: 1, LeftUntilDone: 50}, dlclients.DLClassQueued},
		{"status 1 check wait finished", transmission.Torrent{Status: 1, IsFinished: true}, dlclients.DLClassNone},
		{"status 1 check wait zero left", transmission.Torrent{Status: 1, LeftUntilDone: 0}, dlclients.DLClassNone},
		{"status 2 check unfinished", transmission.Torrent{Status: 2, LeftUntilDone: 50}, dlclients.DLClassQueued},
		{"status 2 check finished", transmission.Torrent{Status: 2, IsFinished: true}, dlclients.DLClassNone},
		{"status 0 stopped", transmission.Torrent{Status: 0}, dlclients.DLClassNone},
		{"status 5 seed wait", transmission.Torrent{Status: 5}, dlclients.DLClassNone},
		{"status 6 seed", transmission.Torrent{Status: 6}, dlclients.DLClassNone},
		{"negative: unknown status", transmission.Torrent{Status: 99}, dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifyTransmission(tc.t); got != tc.want {
				t.Errorf("ClassifyTransmission(%+v) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func transmissionTestServer(torrents []map[string]any) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transmission/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "sess")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "session-get" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": "success", "arguments": map[string]any{"version": "4.1.4"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": "success", "arguments": map[string]any{"torrents": torrents},
		})
	})
	return httptest.NewServer(mux)
}

func TestTransmission_DocumentedStatesHTTP(t *testing.T) {
	t.Parallel()
	allStates := []map[string]any{
		{"id": 1, "status": 4, "error": 0, "isStalled": false},
		{"id": 2, "status": 4, "error": 0, "isStalled": true},
		{"id": 3, "status": 3, "error": 0},
		{"id": 4, "status": 1, "error": 0, "leftUntilDone": 100, "isFinished": false},
		{"id": 5, "status": 1, "error": 0, "leftUntilDone": 0, "isFinished": true},
		{"id": 6, "status": 2, "error": 0, "leftUntilDone": 100, "isFinished": false},
		{"id": 7, "status": 2, "error": 0, "leftUntilDone": 0, "isFinished": true},
		{"id": 8, "status": 0, "error": 0},
		{"id": 9, "status": 5, "error": 0},
		{"id": 10, "status": 6, "error": 0},
		{"id": 11, "status": 4, "error": 1},
		{"id": 12, "status": 3, "error": 2},
		{"id": 13, "status": 99, "error": 0},
	}
	cases := []struct {
		name       string
		torrents   []map[string]any
		wantActive int
		wantQueued int
	}{
		{"all documented states", allStates, 1, 4},
		{"boundary: empty list", []map[string]any{}, 0, 0},
		{"boundary: single downloading", []map[string]any{{"id": 1, "status": 4, "isStalled": false}}, 1, 0},
		{"boundary: all stopped", []map[string]any{{"id": 1, "status": 0}}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := transmissionTestServer(tc.torrents)
			defer srv.Close()
			s := observe(t, domain.ClientTransmission, srv.URL, "", "")
			if !s.Reachable || s.Active != tc.wantActive || s.Queued != tc.wantQueued {
				t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=%d, queued=%d",
					s.Reachable, s.Active, s.Queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}

func TestDeluge_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		t    *deluge.Torrent
		want dlclients.DLClass
	}{
		{"Downloading", &deluge.Torrent{State: "Downloading"}, dlclients.DLClassActive},
		{"Queued unfinished", &deluge.Torrent{State: "Queued", IsFinished: false, IsSeed: false}, dlclients.DLClassQueued},
		{"Queued seed finished", &deluge.Torrent{State: "Queued", IsFinished: true, IsSeed: true}, dlclients.DLClassNone},
		{"Checking unfinished", &deluge.Torrent{State: "Checking", IsFinished: false, IsSeed: false}, dlclients.DLClassQueued},
		{"Checking finished", &deluge.Torrent{State: "Checking", IsFinished: true}, dlclients.DLClassNone},
		{"Allocating unfinished", &deluge.Torrent{State: "Allocating", IsFinished: false, IsSeed: false}, dlclients.DLClassQueued},
		{"Allocating finished", &deluge.Torrent{State: "Allocating", IsFinished: true}, dlclients.DLClassNone},
		{"Paused", &deluge.Torrent{State: "Paused"}, dlclients.DLClassNone},
		{"Moving", &deluge.Torrent{State: "Moving"}, dlclients.DLClassNone},
		{"Seeding", &deluge.Torrent{State: "Seeding"}, dlclients.DLClassNone},
		{"Error", &deluge.Torrent{State: "Error"}, dlclients.DLClassNone},
		{"negative: unknown", &deluge.Torrent{State: "UnknownState"}, dlclients.DLClassNone},
		{"negative: nil", nil, dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifyDeluge(tc.t); got != tc.want {
				t.Errorf("ClassifyDeluge(%+v) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func delugeTestServer(torrents map[string]any) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /json", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int    `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		reply := func(res any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": res, "error": nil, "id": req.ID})
		}
		switch req.Method {
		case "auth.login":
			reply(true)
		case "web.connected":
			reply(true)
		case "daemon.get_version":
			reply("2.2.0")
		case "core.get_torrents_status":
			reply(torrents)
		default:
			reply(nil)
		}
	})
	return httptest.NewServer(mux)
}

func TestDeluge_DocumentedStatesHTTP(t *testing.T) {
	t.Parallel()
	allStates := map[string]any{
		"1":  map[string]any{"state": "Downloading"},
		"2":  map[string]any{"state": "Queued", "is_finished": false, "is_seed": false},
		"3":  map[string]any{"state": "Queued", "is_finished": true, "is_seed": true},
		"4":  map[string]any{"state": "Checking", "is_finished": false, "is_seed": false},
		"5":  map[string]any{"state": "Checking", "is_finished": true, "is_seed": false},
		"6":  map[string]any{"state": "Allocating", "is_finished": false, "is_seed": false},
		"7":  map[string]any{"state": "Allocating", "is_finished": true, "is_seed": false},
		"8":  map[string]any{"state": "Paused"},
		"9":  map[string]any{"state": "Moving"},
		"10": map[string]any{"state": "Seeding"},
		"11": map[string]any{"state": "Error"},
		"12": map[string]any{"state": "CustomUnknown"},
	}
	cases := []struct {
		name       string
		torrents   map[string]any
		wantActive int
		wantQueued int
	}{
		{"all documented states", allStates, 1, 3},
		{"boundary: empty list", map[string]any{}, 0, 0},
		{"boundary: single downloading", map[string]any{"1": map[string]any{"state": "Downloading"}}, 1, 0},
		{"boundary: all paused", map[string]any{"1": map[string]any{"state": "Paused"}}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := delugeTestServer(tc.torrents)
			defer srv.Close()
			s := observe(t, domain.ClientDeluge, srv.URL, "", "")
			if !s.Reachable || s.Active != tc.wantActive || s.Queued != tc.wantQueued {
				t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=%d, queued=%d",
					s.Reachable, s.Active, s.Queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}

func TestRTorrent_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		t    rtorrent.Torrent
		want dlclients.DLClass
	}{
		{"active: open, active, incomplete", rtorrent.Torrent{IsOpen: true, IsActive: true, IsComplete: false}, dlclients.DLClassActive},
		{"queued: incomplete hash checking", rtorrent.Torrent{IsOpen: true, IsActive: false, IsComplete: false, IsHashChecking: true}, dlclients.DLClassQueued},
		{"queued: closed incomplete hash checking", rtorrent.Torrent{IsOpen: false, IsActive: false, IsComplete: false, IsHashChecking: true}, dlclients.DLClassQueued},
		{"none: stopped/closed incomplete", rtorrent.Torrent{IsOpen: false, IsActive: false, IsComplete: false, IsHashChecking: false}, dlclients.DLClassNone},
		{"none: paused open incomplete", rtorrent.Torrent{IsOpen: true, IsActive: false, IsComplete: false, IsHashChecking: false}, dlclients.DLClassNone},
		{"none: complete seeding", rtorrent.Torrent{IsOpen: true, IsActive: true, IsComplete: true}, dlclients.DLClassNone},
		{"none: complete paused", rtorrent.Torrent{IsOpen: true, IsActive: false, IsComplete: true}, dlclients.DLClassNone},
		{"none: complete closed", rtorrent.Torrent{IsOpen: false, IsActive: false, IsComplete: true}, dlclients.DLClassNone},
		{"none: complete hash checking", rtorrent.Torrent{IsOpen: true, IsActive: false, IsComplete: true, IsHashChecking: true}, dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifyRTorrent(tc.t); got != tc.want {
				t.Errorf("ClassifyRTorrent(%+v) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func rtorrentRow(open, active, complete, hashChecking bool) string {
	flag := func(b bool) string {
		if b {
			return "<value><i8>1</i8></value>"
		}
		return "<value><i8>0</i8></value>"
	}
	fields := []string{
		"<value><string>0123456789ABCDEF0123456789ABCDEF01234567</string></value>",
		"<value><string>a download</string></value>",
		"<value><i8>4000000000</i8></value>",
		"<value><i8>1000000000</i8></value>",
		"<value><i8>0</i8></value>",
		"<value><i8>0</i8></value>",
		"<value><i8>0</i8></value>",
		"<value><i8>0</i8></value>",
		flag(open),
		flag(active),
		flag(complete),
		flag(hashChecking),
		"<value><string>/downloads</string></value>",
		"<value><string>/downloads</string></value>",
		"<value><string></string></value>",
		"<value><i8>1760000000</i8></value>",
		"<value><string></string></value>",
	}
	return "<value><array><data>" + strings.Join(fields, "") + "</data></array></value>"
}

func rtorrentTestServer(torrentsXML string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string `xml:"methodName"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&call)
		res := ""
		switch call.Method {
		case "system.client_version":
			res = "<params><param><value><string>0.16.26</string></value></param></params>"
		case "system.library_version":
			res = "<params><param><value><string>0.13.8</string></value></param></params>"
		case "system.hostname":
			res = "<params><param><value><string>box</string></value></param></params>"
		case "system.pid":
			res = "<params><param><value><i8>1234</i8></value></param></params>"
		case "d.multicall2":
			res = "<params><param><value><array><data>" + torrentsXML + "</data></array></value></param></params>"
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, xml.Header+"<methodResponse>"+res+"</methodResponse>")
	}))
}

func TestRTorrent_DocumentedStatesHTTP(t *testing.T) {
	t.Parallel()
	allRows := strings.Join([]string{
		rtorrentRow(true, true, false, false),   // active
		rtorrentRow(true, false, false, true),   // queued (hash checking)
		rtorrentRow(false, false, false, false), // none (stopped)
		rtorrentRow(true, false, false, false),  // none (paused)
		rtorrentRow(true, true, true, false),    // none (seeding)
		rtorrentRow(true, false, true, true),    // none (complete hashing)
	}, "")

	cases := []struct {
		name       string
		xmlRows    string
		wantActive int
		wantQueued int
	}{
		{"all documented states", allRows, 1, 1},
		{"boundary: empty list", "", 0, 0},
		{"boundary: single downloading", rtorrentRow(true, true, false, false), 1, 0},
		{"boundary: all closed/paused", rtorrentRow(false, false, false, false) + rtorrentRow(true, false, false, false), 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := rtorrentTestServer(tc.xmlRows)
			defer srv.Close()
			s := observe(t, domain.ClientRTorrent, srv.URL, "", "")
			if !s.Reachable || s.Active != tc.wantActive || s.Queued != tc.wantQueued {
				t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=%d, queued=%d",
					s.Reachable, s.Active, s.Queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}

func TestSABnzbd_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   dlclients.DLClass
	}{
		{"Downloading", dlclients.DLClassActive},
		{"Fetching", dlclients.DLClassActive},
		{"Checking", dlclients.DLClassQueued},
		{"Grabbing", dlclients.DLClassQueued},
		{"Propagating", dlclients.DLClassQueued},
		{"Queued", dlclients.DLClassQueued},
		{"Paused", dlclients.DLClassNone},
		{"Deleted", dlclients.DLClassNone},
		{"Idle", dlclients.DLClassNone},
		{"QuickCheck", dlclients.DLClassNone},
		{"Verifying", dlclients.DLClassNone},
		{"Repairing", dlclients.DLClassNone},
		{"Extracting", dlclients.DLClassNone},
		{"Moving", dlclients.DLClassNone},
		{"Running", dlclients.DLClassNone},
		{"Completed", dlclients.DLClassNone},
		{"Failed", dlclients.DLClassNone},
		{"UnknownStatus", dlclients.DLClassNone},
		{"", dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifySABStatus(tc.status); got != tc.want {
				t.Errorf("ClassifySABStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestSABnzbd_SlotList(t *testing.T) {
	t.Parallel()
	// A SABnzbd 5.1.3 queue with pre-check on: one job being checked, three waiting for the check,
	// the head job and two more marked Downloading, one URL grab, one propagation delay, one paused.
	preCheckQueue := []sabnzbd.Slot{
		{Status: "Checking"},
		{Status: "Checking"},
		{Status: "Checking"},
		{Status: "Checking"},
		{Status: "Downloading"},
		{Status: "Downloading"},
		{Status: "Downloading"},
		{Status: "Grabbing"},
		{Status: "Propagating"},
		{Status: "Paused"},
	}
	cases := []struct {
		name       string
		slots      []sabnzbd.Slot
		wantActive int
		wantQueued int
	}{
		{"pre-check queue", preCheckQueue, 1, 8},
		{"boundary: single downloading", []sabnzbd.Slot{{Status: "Downloading"}}, 1, 0},
		{"boundary: multiple downloading slots", []sabnzbd.Slot{{Status: "Downloading"}, {Status: "Downloading"}, {Status: "Downloading"}}, 1, 2},
		{"boundary: all paused", []sabnzbd.Slot{{Status: "Paused"}, {Status: "Paused"}}, 0, 0},
		{"boundary: empty list", []sabnzbd.Slot{}, 0, 0},
		{"only checking and queued", []sabnzbd.Slot{{Status: "Checking"}, {Status: "Queued"}}, 0, 2},
		{"fetching head", []sabnzbd.Slot{{Status: "Fetching"}, {Status: "Downloading"}}, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			active, queued := dlclients.ClassifySAB(tc.slots)
			if active != tc.wantActive || queued != tc.wantQueued {
				t.Fatalf("ClassifySAB() = (%d, %d), want (%d, %d)", active, queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}

func TestSABnzbd_PreCheckFixtureHTTP(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		w.Header().Set("Content-Type", "application/json")
		if mode == "version" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "5.1.3"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queue": map[string]any{
				"paused": false,
				"slots": []map[string]any{
					{"status": "Checking"},
					{"status": "Checking"},
					{"status": "Checking"},
					{"status": "Checking"},
					{"status": "Downloading"},
					{"status": "Downloading"},
					{"status": "Downloading"},
					{"status": "Grabbing"},
					{"status": "Propagating"},
					{"status": "Paused"},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := observe(t, domain.ClientSABnzbd, srv.URL, "", "")
	if !s.Reachable || s.Active != 1 || s.Queued != 8 {
		t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=1, queued=8",
			s.Reachable, s.Active, s.Queued)
	}
}

func TestNZBGet_StateClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   dlclients.DLClass
	}{
		{"DOWNLOADING", dlclients.DLClassActive},
		{"FETCHING", dlclients.DLClassQueued},
		{"QUEUED", dlclients.DLClassQueued},
		{"PAUSED", dlclients.DLClassNone},
		{"QS_QUEUED", dlclients.DLClassNone},
		{"QS_EXECUTING", dlclients.DLClassNone},
		{"PP_QUEUED", dlclients.DLClassNone},
		{"LOADING_PARS", dlclients.DLClassNone},
		{"VERIFYING_SOURCES", dlclients.DLClassNone},
		{"REPAIRING", dlclients.DLClassNone},
		{"VERIFYING_REPAIRED", dlclients.DLClassNone},
		{"RENAMING", dlclients.DLClassNone},
		{"UNPACKING", dlclients.DLClassNone},
		{"MOVING", dlclients.DLClassNone},
		{"POST_UNPACK_RENAMING", dlclients.DLClassNone},
		{"EXECUTING_SCRIPT", dlclients.DLClassNone},
		{"PP_FINISHED", dlclients.DLClassNone},
		{"UNKNOWN", dlclients.DLClassNone},
		{"", dlclients.DLClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			if got := dlclients.ClassifyNZBGet(tc.status); got != tc.want {
				t.Errorf("ClassifyNZBGet(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func nzbgetTestServer(groups []map[string]any) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "version":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "26.3"})
		case "status":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"DownloadPaused": false}})
		case "listgroups":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": groups})
		}
	})
	return httptest.NewServer(mux)
}

func TestNZBGet_DocumentedStatesHTTP(t *testing.T) {
	t.Parallel()
	allStates := []map[string]any{
		{"Status": "DOWNLOADING", "ActiveDownloads": 2},
		{"Status": "FETCHING", "ActiveDownloads": 1}, // ActiveDownloads > 0, but FETCHING must be queued
		{"Status": "QUEUED", "ActiveDownloads": 0},
		{"Status": "PAUSED"},
		{"Status": "QS_QUEUED"},
		{"Status": "QS_EXECUTING"},
		{"Status": "PP_QUEUED"},
		{"Status": "LOADING_PARS"},
		{"Status": "VERIFYING_SOURCES"},
		{"Status": "REPAIRING"},
		{"Status": "VERIFYING_REPAIRED"},
		{"Status": "RENAMING"},
		{"Status": "UNPACKING"},
		{"Status": "MOVING"},
		{"Status": "POST_UNPACK_RENAMING"},
		{"Status": "EXECUTING_SCRIPT"},
		{"Status": "PP_FINISHED"},
		{"Status": "UNKNOWN_CUSTOM"},
	}
	cases := []struct {
		name       string
		groups     []map[string]any
		wantActive int
		wantQueued int
	}{
		{"all documented states", allStates, 1, 2},
		{"boundary: empty list", []map[string]any{}, 0, 0},
		{"boundary: single downloading", []map[string]any{{"Status": "DOWNLOADING"}}, 1, 0},
		{"boundary: all paused", []map[string]any{{"Status": "PAUSED"}}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := nzbgetTestServer(tc.groups)
			defer srv.Close()
			s := observe(t, domain.ClientNZBGet, srv.URL, "", "")
			if !s.Reachable || s.Active != tc.wantActive || s.Queued != tc.wantQueued {
				t.Fatalf("got reachable=%t, active=%d, queued=%d; want active=%d, queued=%d",
					s.Reachable, s.Active, s.Queued, tc.wantActive, tc.wantQueued)
			}
		})
	}
}
