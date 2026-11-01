// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type priorityFake struct {
	mu        sync.Mutex
	requests  []string
	forbidden []string
	fail      bool
	disabled  bool
}

func (f *priorityFake) serve(w http.ResponseWriter, r *http.Request) {
	path := priorityReadRequest(r)
	f.mu.Lock()
	f.requests = append(f.requests, path)
	if path == "" {
		f.forbidden = append(f.forbidden, r.Method+" "+r.URL.Path)
	}
	f.mu.Unlock()
	if path == "" {
		http.Error(w, "forbidden", http.StatusBadRequest)
		return
	}
	if f.fail {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch path {
	case "login":
		fmt.Fprint(w, "Ok.")
	case "preferences":
		fmt.Fprintf(w, `{"queueing_enabled":%t}`, !f.disabled)
	case "torrents":
		first, second := 2, 1
		if f.disabled {
			first, second = -1, 0
		}
		fmt.Fprintf(w, `[{"hash":"best","category":"movies","state":"queuedDL","priority":%d,"added_on":100,"name":"private title"},{"hash":"worst","category":"tv","state":"downloading","priority":%d,"added_on":200},{"hash":"unknown","category":"other","state":"pausedDL","priority":-1}]`, first, second)
	case "listgroups":
		fmt.Fprint(w, `{"version":"1.1","result":[{"NZBID":1,"Category":"movies","Status":"QUEUED","MaxPriority":0},{"NZBID":2,"Category":"tv","Status":"DOWNLOADING","MaxPriority":50},{"NZBID":3,"Category":"other","Status":"PAUSED","MaxPriority":-50}]}`)
	case "queue":
		fmt.Fprint(w, `{"queue":{"slots":[{"nzo_id":"best","cat":"movies","status":"Queued","priority":"Normal","filename":"private title"},{"nzo_id":"worst","cat":"tv","status":"Downloading","priority":"High"},{"nzo_id":"unknown","cat":"other","status":"Paused","priority":"-1"}]}}`)
	}
}

// Explicit wire allow-list guards reads independently of the adapter interface.
func priorityReadRequest(r *http.Request) string {
	switch r.Method {
	case http.MethodGet:
		return priorityReadGET(r)
	case http.MethodPost:
		return priorityReadPOST(r)
	default:
		return ""
	}
}

func priorityReadGET(r *http.Request) string {
	switch r.URL.Path {
	case "/api/v2/app/preferences":
		return "preferences"
	case "/api/v2/torrents/info":
		return "torrents"
	case "/api":
		q := r.URL.Query()
		if q.Get("mode") == "queue" && !q.Has("name") && !q.Has("value") {
			return "queue"
		}
	}
	return ""
}

func priorityReadPOST(r *http.Request) string {
	if r.URL.Path == "/api/v2/auth/login" {
		return "login"
	}
	if r.URL.Path == "/jsonrpc" {
		var body struct {
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Method == "listgroups" {
			return "listgroups"
		}
	}
	return ""
}

func TestPriorityItemsMapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		kind           domain.DownloadClientKind
		disabled, fail bool
		id             string
		priority       int
	}{
		{"qbit", domain.ClientQBittorrent, false, false, "best", 2},
		{"qbit queueing disabled", domain.ClientQBittorrent, true, false, "best", -1},
		{"nzbget", domain.ClientNZBGet, false, false, "1", 0},
		{"sab", domain.ClientSABnzbd, false, false, "best", 0},
		{"qbit failure", domain.ClientQBittorrent, false, true, "", 0},
		{"nzbget failure", domain.ClientNZBGet, false, true, "", 0},
		{"sab failure", domain.ClientSABnzbd, false, true, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &priorityFake{disabled: tc.disabled, fail: tc.fail}
			server := httptest.NewServer(http.HandlerFunc(fake.serve))
			defer server.Close()
			got, err := NewGoenvoy(clock.NewFake()).PriorityItems(t.Context(), domain.DownloadClient{Kind: tc.kind, BaseURL: server.URL}, "")
			if tc.fail {
				if err == nil {
					t.Fatal("expected read failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertPriorityMapping(t, got, tc.id, tc.kind, tc.priority, tc.disabled)
			fake.assertReads(t)
		})
	}
}

func (f *priorityFake) assertReads(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 || len(f.forbidden) > 0 {
		t.Fatalf("requests = %v; forbidden = %v", f.requests, f.forbidden)
	}
}

func TestPriorityReadAllowListPlantedWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ method, target, body string }{
		{"POST", "/api/v2/torrents/topPrio", ""},
		{"POST", "/api/v2/torrents/pause", ""},
		{"GET", "/api?mode=queue&name=priority&value=best", ""},
		{"GET", "/api?mode=pause", ""},
		{"GET", "/api?mode=queue&name=", ""},
		{"POST", "/jsonrpc", `{"method":"editqueue","params":["GroupPause",0,"",[1]]}`},
	} {
		t.Run(tc.target+tc.body, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			if got := priorityReadRequest(req); got != "" {
				t.Fatalf("planted write allowed as %q", got)
			}
		})
	}
}

func TestPriorityItemsDeadline(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.DownloadClientKind{domain.ClientQBittorrent, domain.ClientNZBGet, domain.ClientSABnzbd} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			defer srv.Close()
			observer := NewGoenvoy(clock.NewFake())
			observer.timeout = 20 * time.Millisecond
			_, err := observer.PriorityItems(context.Background(), domain.DownloadClient{Kind: kind, BaseURL: srv.URL}, "")
			if err == nil {
				t.Fatal("expected bounded read failure")
			}
		})
	}
}

func TestSABPriorityValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		value int
		valid bool
	}{{"Normal", 0, true}, {"High", 1, true}, {"Low", -1, true}, {"Force", 2, true}, {"Repair", 3, true}, {"-100", -100, true}, {"unknown", 0, false}, {"", 0, false}} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := sabPriority(tc.input)
			if got != tc.value || (err == nil) != tc.valid {
				t.Fatalf("priority = %d, err = %v", got, err)
			}
		})
	}
}

func TestSABHeadSlot(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"queue":{"slots":[{"nzo_id":"a","status":"Downloading","priority":"Normal"},{"nzo_id":"b","status":"Downloading","priority":"Normal"},{"nzo_id":"c","status":"Fetching","priority":"Normal"},{"nzo_id":"d","status":"Paused","priority":"Low"},{"nzo_id":"e","status":"Completed","priority":"Normal"}]}}`)
	}))
	defer server.Close()
	got, err := NewGoenvoy(clock.NewFake()).PriorityItems(t.Context(), domain.DownloadClient{Kind: domain.ClientSABnzbd, BaseURL: server.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DownloadItemState{domain.DownloadItemActive, domain.DownloadItemQueued, domain.DownloadItemQueued, domain.DownloadItemPaused, domain.DownloadItemFinished}
	for i, state := range want {
		if got.Items[i].State != state {
			t.Errorf("slot %d state = %s, want %s", i, got.Items[i].State, state)
		}
	}
}

func assertPriorityMapping(t *testing.T, got PriorityObservation, id string, kind domain.DownloadClientKind, priority int, disabled bool) {
	t.Helper()
	if len(got.Items) != 3 {
		t.Fatalf("items = %+v", got.Items)
	}
	want := PriorityItem{Category: "movies", ID: id, Priority: priority, State: domain.DownloadItemQueued}
	if kind == domain.ClientQBittorrent {
		want.Added = time.Unix(100, 0).UTC()
	}
	if !reflect.DeepEqual(got.Items[0], want) {
		t.Fatalf("item = %+v, want %+v", got.Items[0], want)
	}
	states := []domain.DownloadItemState{got.Items[0].State, got.Items[1].State, got.Items[2].State}
	if !reflect.DeepEqual(states, []domain.DownloadItemState{domain.DownloadItemQueued, domain.DownloadItemActive, domain.DownloadItemPaused}) {
		t.Fatalf("states = %v", states)
	}
	if got.QueueingDisabled != disabled {
		t.Fatalf("queueing disabled = %v", got.QueueingDisabled)
	}
}
