// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
)

func TestRouterListEventsFilters(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	inst := seedTestInstance(t, f.integrationFixture)
	seedEventRecord(t, f.integrationFixture, inst, "search_dispatched", "search")
	seedEventRecord(t, f.integrationFixture, inst, "run_queued", "queued")
	cases := []struct {
		name  string
		query string
		count int
		want  int
	}{
		{"positive no filter", "", 2, http.StatusOK},
		{"positive proto type", "type=search_dispatched", 1, http.StatusOK},
		{"positive api type", "type=run_queued", 1, http.StatusOK},
		{"boundary known type without matches", "type=queue_item_imported", 0, http.StatusOK},
		{"negative unknown type", "type=nope", 0, http.StatusBadRequest},
		{"negative noncanonical type", "type=EVENT_TYPE_SEARCH_DISPATCHED", 0, http.StatusBadRequest},
		{"boundary empty type", "type=", 0, http.StatusBadRequest},
		{"boundary cursor one", "before_id=1", 0, http.StatusOK},
		{"boundary cursor zero", "before_id=0", 0, http.StatusBadRequest},
		{"negative cursor", "before_id=-5", 0, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/events?"+tc.query, "", nil)
			if tc.want != http.StatusOK {
				assertProblemStatusAndType(t, res, tc.want)
				return
			}
			var rows []oas.Event
			if err := json.Unmarshal(res.body, &rows); err != nil || res.statusCode != tc.want || len(rows) != tc.count {
				t.Fatalf("GET /events?%s = %d %s, err %v; want %d, %d events", tc.query, res.statusCode, res.body, err, tc.want, tc.count)
			}
		})
	}
}

func TestRouterListLogsFilters(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"positive no filter", "", http.StatusOK},
		{"positive api", "source=api", http.StatusOK},
		{"positive all workers", "source=worker", http.StatusOK},
		{"positive worker id", "source=worker:abc", http.StatusOK},
		{"negative unknown source", "source=nope", http.StatusBadRequest},
		{"boundary empty source", "source=", http.StatusBadRequest},
		{"boundary empty worker id", "source=worker:", http.StatusBadRequest},
		{"negative whitespace worker id", "source=worker:a%20b", http.StatusBadRequest},
		{"negative colon worker id", "source=worker:a:b", http.StatusBadRequest},
		{"negative level", "level=nope", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/logs?"+tc.query, "", nil)
			if tc.want != http.StatusOK {
				assertProblemStatusAndType(t, res, tc.want)
				return
			}
			var page oas.LogPage
			if err := json.Unmarshal(res.body, &page); err != nil || res.statusCode != tc.want {
				t.Fatalf("GET /logs?%s = %d %s, err %v; want %d", tc.query, res.statusCode, res.body, err, tc.want)
			}
		})
	}
}
