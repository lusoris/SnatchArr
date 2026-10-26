// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/metadata"
	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func TestDeduplicatePerformers(t *testing.T) {
	t.Parallel()
	items := []stashbox.Performer{
		{
			ID:   "p1",
			Name: "Kira Noir",
			URLs: []stashbox.URLEntry{
				{URL: "https://twitter.com/kiranoir"},
			},
		},
		{
			ID:   "p2",
			Name: "Kira Noir",
			URLs: []stashbox.URLEntry{
				{URL: "http://www.twitter.com/kiranoir/"},
			},
		},
		{
			ID:   "p3",
			Name: "Alex Adams",
			URLs: []stashbox.URLEntry{
				{URL: "https://iafd.com/person/alex"},
			},
		},
	}

	deduped := deduplicatePerformers(items, nil)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 performers after deduplication, got %d", len(deduped))
	}
	if deduped[0].Name != "Kira Noir" || deduped[1].Name != "Alex Adams" {
		t.Errorf("unexpected performers in deduped list: %+v", deduped)
	}
}

func TestDeduplicateStudios(t *testing.T) {
	t.Parallel()
	items := []stashbox.Studio{
		{
			ID:   "s1",
			Name: "Brazzers",
			URLs: []stashbox.URLEntry{
				{URL: "https://brazzers.com"},
			},
		},
		{
			ID:   "s2",
			Name: "Brazzers",
			URLs: []stashbox.URLEntry{
				{URL: "http://www.brazzers.com/"},
			},
		},
		{
			ID:   "s3",
			Name: "Evil Angel",
			URLs: []stashbox.URLEntry{
				{URL: "https://evilangel.com"},
			},
		},
	}

	deduped := deduplicateStudios(items, nil)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 studios after deduplication, got %d", len(deduped))
	}
	if deduped[0].Name != "Brazzers" || deduped[1].Name != "Evil Angel" {
		t.Errorf("unexpected studios in deduped list: %+v", deduped)
	}
}

func TestSearchInstance_PartialFailure_WarnLoggedAndResultsReturned(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Query, "searchPerformer") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"searchPerformer": []any{
						map[string]any{"id": "p1", "name": "Kira Noir"},
					},
				},
			})
			return
		}
		// searchStudio fails with GraphQL error
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []any{
				map[string]any{"message": "studio search disabled"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := stashbox.New(srv.URL, "key", metadata.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("stashbox.New: %v", err)
	}

	instID := uuid.MustParse("00000000-0000-0000-0000-000000000055")
	ic := InstanceClient{
		Instance: sqlcgen.AdultStashboxInstance{
			ID:   instID,
			Name: "Test Instance",
		},
		Client: client,
	}

	svc := &Service{logger: slog.Default()}
	res := svc.searchInstance(context.Background(), ic, "kira")
	if res.err != nil {
		t.Fatalf("expected nil err for partial failure, got: %v", res.err)
	}
	if len(res.performers) != 1 || res.performers[0].Name != "Kira Noir" {
		t.Errorf("expected 1 performer returned, got: %+v", res.performers)
	}
	if len(res.studios) != 0 {
		t.Errorf("expected 0 studios returned, got: %+v", res.studios)
	}
}
