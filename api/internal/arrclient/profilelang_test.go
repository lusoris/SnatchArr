// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

func TestProfileLanguages(t *testing.T) {
	t.Parallel()

	t.Run("positive language present, null and absent, sorted and method GET", func(t *testing.T) {
		t.Parallel()
		var requestCount atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			if r.Method != http.MethodGet {
				t.Errorf("r.Method = %s, want GET", r.Method)
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path != "/api/v3/qualityprofile" {
				t.Errorf("r.URL.Path = %s, want /api/v3/qualityprofile", r.URL.Path)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id": 2, "name": "Ultra", "language": {"id": 4, "name": "German"}},
				{"id": 1, "name": "any", "language": {"id": -1, "name": "Any"}},
				{"id": 3, "name": "null_lang", "language": null},
				{"id": 4, "name": "absent_lang"},
				{"id": 0, "name": "ignored_zero"}
			]`))
		}))
		defer srv.Close()

		p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
		got, err := p.ProfileLanguages(t.Context(), domain.KindRadarr, srv.URL, "test-api-key")
		if err != nil {
			t.Fatalf("ProfileLanguages() error = %v", err)
		}

		want := []arrclient.ProfileLanguage{
			{ID: 4, Name: "absent_lang", Language: nil},
			{ID: 1, Name: "any", Language: &foreigntitle.RadarrLanguage{ID: -1, Name: "Any"}},
			{ID: 3, Name: "null_lang", Language: nil},
			{ID: 2, Name: "Ultra", Language: &foreigntitle.RadarrLanguage{ID: 4, Name: "German"}},
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ProfileLanguages() =\n%+v\nwant:\n%+v", got, want)
		}
		if requestCount.Load() != 1 {
			t.Errorf("requestCount = %d, want 1", requestCount.Load())
		}
	})

	t.Run("negative non-Radarr kind refused without request", func(t *testing.T) {
		t.Parallel()
		var requestCount atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
		_, err := p.ProfileLanguages(t.Context(), domain.KindSonarr, srv.URL, "key")
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ProfileLanguages() err = %v, want ErrInvalid", err)
		}
		if requestCount.Load() != 0 {
			t.Fatalf("expected 0 requests to server, got %d", requestCount.Load())
		}
	})

	t.Run("negative http 500 wraps ErrUnreachable", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
		_, err := p.ProfileLanguages(t.Context(), domain.KindRadarr, srv.URL, "key")
		if !errors.Is(err, domain.ErrUnreachable) {
			t.Fatalf("ProfileLanguages() err = %v, want ErrUnreachable", err)
		}
	})
}

func TestProfileLanguageIDBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		id   int64
		keep bool
	}{
		{"positive", 4, true},
		{"minimum", math.MinInt32, true},
		{"maximum", math.MaxInt32, true},
		{"below", math.MinInt32 - 1, false},
		{"above", math.MaxInt32 + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `[{"id":1,"name":"profile","language":{"id":%d,"name":"lang"}}]`, tc.id)
			}))
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			got, err := p.ProfileLanguages(t.Context(), domain.KindRadarr, srv.URL, "test-key")
			if err != nil || len(got) != 1 {
				t.Fatalf("profiles = %v, %v", got, err)
			}
			if (got[0].Language != nil) != tc.keep {
				t.Fatalf("language = %v; keep %t", got[0].Language, tc.keep)
			}
			if tc.keep && int64(got[0].Language.ID) != tc.id {
				t.Fatalf("language ID = %d; want %d", got[0].Language.ID, tc.id)
			}
			if !tc.keep {
				_, reason := foreigntitle.RadarrProfileVerdict(got[0].Language, "de")
				if reason != foreigntitle.ReasonProfileLanguageMissing {
					t.Fatalf("reason = %s", reason)
				}
			}
		})
	}
}

func TestProfileBodyCap(t *testing.T) {
	t.Parallel()
	const capBytes = 8 << 20
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"below", capBytes - 1, false}, {"exact", capBytes, false}, {"above", capBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const profile = `[{"id":1,"name":"profile","language":{"id":4,"name":"German"}}]`
			body := profile + strings.Repeat(" ", tc.size-len(profile))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 5 * time.Second})
			got, err := p.ProfileLanguages(t.Context(), domain.KindRadarr, srv.URL, "test-key")
			if tc.wantErr {
				if !errors.Is(err, domain.ErrUnreachable) {
					t.Fatalf("error = %v; want ErrUnreachable", err)
				}
			} else if err != nil || len(got) != 1 || got[0].Language.ID != 4 {
				t.Fatalf("profiles = %v, %v", got, err)
			}
		})
	}
}

func TestProfileCountBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		count   int
		wantErr bool
	}{
		{"empty", 0, false}, {"maximum", 1000, false}, {"above", 1001, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				var body strings.Builder
				body.WriteString("[")
				for i := 1; i <= tc.count; i++ {
					if i > 1 {
						body.WriteString(",")
					}
					fmt.Fprintf(&body, `{"id":%d,"name":"profile"}`, i)
				}
				body.WriteString("]")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body.String()))
			}))
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			got, err := p.ProfileLanguages(t.Context(), domain.KindRadarr, srv.URL, "test-key")
			if tc.wantErr {
				if !errors.Is(err, domain.ErrUnreachable) || errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("error = %v; want ErrUnreachable only", err)
				}
			} else if err != nil || len(got) != tc.count {
				t.Fatalf("profiles len = %d, error %v; want %d", len(got), err, tc.count)
			}
		})
	}
}
