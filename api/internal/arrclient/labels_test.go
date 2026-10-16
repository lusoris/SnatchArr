// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// fakeLabels answers /api/v{1,3}/tag and /qualityprofile; failing names the path that fails.
func fakeLabels(t *testing.T, failing string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		path = strings.TrimPrefix(strings.TrimPrefix(path, "/api/v3"), "/api/v1")
		if failing != "" && path == failing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch path {
		case "/tag":
			_, _ = w.Write([]byte(`[{"id":3,"label":"b"},{"id":2,"label":"a"},{"id":1,"label":"A"},{"id":0,"label":"zero"}]`))
		case "/qualityprofile":
			_, _ = w.Write([]byte(`[{"id":7,"name":"Ultra"},{"id":4,"name":"Any"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLabels(t *testing.T) {
	t.Parallel()
	want := arrclient.Labels{
		// Names compare without case, equal names by id; id 0 cannot be stored in a policy.
		Tags:     []arrclient.Label{{ID: 1, Name: "A"}, {ID: 2, Name: "a"}, {ID: 3, Name: "b"}},
		Profiles: []arrclient.Label{{ID: 4, Name: "Any"}, {ID: 7, Name: "Ultra"}},
	}
	tests := []struct {
		name    string
		kind    domain.AppKind
		failing string
		wantErr bool
	}{
		{"positive sonarr", domain.KindSonarr, "", false},
		{"positive readarr on api v1", domain.KindReadarr, "", false},
		{"positive whisparr v3", domain.KindWhisparrV3, "", false},
		{"negative tags fail", domain.KindRadarr, "/tag", true},
		{"negative quality profiles fail", domain.KindLidarr, "/qualityprofile", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := fakeLabels(t, tc.failing)
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			got, err := p.Labels(t.Context(), tc.kind, srv.URL, "k")
			if tc.wantErr {
				if !errors.Is(err, domain.ErrUnreachable) {
					t.Fatalf("err = %v, want unreachable", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("Labels() = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestLabelsOfAnUnknownKind(t *testing.T) {
	t.Parallel()
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
	if _, err := p.Labels(t.Context(), domain.AppKind("plex"), "http://127.0.0.1:1", "k"); err == nil {
		t.Fatal("Labels() of an unknown kind succeeded")
	}
}
