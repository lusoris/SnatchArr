// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golusoris/goenvoy/mediaserver/jellyfin/v3"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

func TestBuildItemParams(t *testing.T) {
	t.Parallel()

	params := buildItemParams("user-42", 10, 50)

	if got := params.Get("userId"); got != "user-42" {
		t.Errorf("userId = %q, want user-42", got)
	}
	if got := params.Get("Recursive"); got != "true" {
		t.Errorf("Recursive = %q, want true", got)
	}
	if got := params.Get("IncludeItemTypes"); got != "Episode,Movie,Series" {
		t.Errorf("IncludeItemTypes = %q, want Episode,Movie,Series", got)
	}
	if got := params.Get("Fields"); got != "ProviderIds" {
		t.Errorf("Fields = %q, want ProviderIds", got)
	}
	if got := params.Get("EnableUserData"); got != "true" {
		t.Errorf("EnableUserData = %q, want true", got)
	}
	if got := params.Get("StartIndex"); got != "10" {
		t.Errorf("StartIndex = %q, want 10", got)
	}
	if got := params.Get("Limit"); got != "50" {
		t.Errorf("Limit = %q, want 50", got)
	}
}

func TestExtractTvdbID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		providers map[string]string
		want      string
	}{
		{
			name:      "exact casing Tvdb",
			providers: map[string]string{"Tvdb": "12345"},
			want:      "12345",
		},
		{
			name:      "lowercase tvdb",
			providers: map[string]string{"tvdb": "67890"},
			want:      "67890",
		},
		{
			name:      "uppercase TVDB",
			providers: map[string]string{"TVDB": "11223"},
			want:      "11223",
		},
		{
			name:      "multiple providers with mixed casing",
			providers: map[string]string{"Imdb": "tt123456", "tvdb": "99999", "Tmdb": "888"},
			want:      "99999",
		},
		{
			name:      "nil map",
			providers: nil,
			want:      "",
		},
		{
			name:      "empty map",
			providers: map[string]string{},
			want:      "",
		},
		{
			name:      "other providers only",
			providers: map[string]string{"Imdb": "tt123", "Tmdb": "456"},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractTvdbID(tt.providers)
			if got != tt.want {
				t.Errorf("extractTvdbID(%v) = %q, want %q", tt.providers, got, tt.want)
			}
		})
	}
}

type stubSettingsReader struct {
	ua string
}

func (r *stubSettingsReader) Get(context.Context) (domain.Settings, error) {
	return domain.Settings{UserAgent: r.ua}, nil
}

func TestNewServiceWithSource_UserAgent(t *testing.T) {
	t.Parallel()

	received := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]jellyfin.UserDto{})
	}))
	t.Cleanup(srv.Close)

	enc, err := crypto.NewEncryptor([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("crypto.NewEncryptor: %v", err)
	}
	sealed, err := enc.Seal([]byte("secret-key"))
	if err != nil {
		t.Fatalf("enc.Seal: %v", err)
	}
	rows := []sqlcgen.WatchawareSource{{
		ID:        uuid.New(),
		Kind:      string(KindJellyfin),
		Name:      "jf",
		Url:       srv.URL,
		ApiKeyEnc: sealed,
		Enabled:   true,
	}}
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	info := buildinfo.Info{Version: "2.3.4"}

	staticSvc := newServiceWithSource(nil, enc, clk, id.New(), logger, nil, nil, nil, nil, nil, info, nil)
	named := staticSvc.buildNamedSources(t.Context(), rows)
	if len(named) != 1 {
		t.Fatalf("len(named) = %d, want 1", len(named))
	}
	if _, err := named[0].Source.Watches(t.Context()); err != nil {
		t.Fatalf("Watches(static): %v", err)
	}
	if got, want := <-received, useragent.Default(info); got != want {
		t.Fatalf("static User-Agent = %q, want %q", got, want)
	}

	reader := &stubSettingsReader{ua: "WatchAwareUA/1.0"}
	uaSource := useragent.New(reader, info, clk, logger)
	dynSvc := newServiceWithSource(nil, enc, clk, id.New(), logger, nil, nil, nil, nil, nil, info, uaSource)
	for _, want := range []string{"WatchAwareUA/1.0", "WatchAwareUA/2.0"} {
		reader.ua = want
		uaSource.Invalidate()
		dynNamed := dynSvc.buildNamedSources(t.Context(), rows)
		if len(dynNamed) != 1 {
			t.Fatalf("len(dynNamed) = %d, want 1", len(dynNamed))
		}
		if _, err := dynNamed[0].Source.Watches(t.Context()); err != nil {
			t.Fatalf("Watches(%s): %v", want, err)
		}
		if got := <-received; got != want {
			t.Fatalf("dynamic User-Agent = %q, want %q", got, want)
		}
	}
}
