// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

type stubUserAgent struct {
	ctx   context.Context
	value string
}

type userAgentContextKey struct{}

func (s *stubUserAgent) Current(ctx context.Context) string {
	s.ctx = ctx
	return s.value
}

func TestServerUserAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider useragent.Provider
		info     buildinfo.Info
		want     string
		contains string
	}{
		{
			name:     "provider override",
			provider: &stubUserAgent{value: "custom/override"},
			want:     "custom/override",
		},
		{
			name:     "versioned fallback",
			info:     buildinfo.Info{Version: "3"},
			want:     useragent.Default(buildinfo.Info{Version: "3"}),
			contains: "SnatchArr/3",
		},
		{
			name:     "development fallback",
			want:     useragent.Default(buildinfo.Info{}),
			contains: "SnatchArr/dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.WithValue(context.Background(), userAgentContextKey{}, "caller")
			srv := &Server{userAgentProvider: tt.provider, info: tt.info}
			if got := srv.userAgent(ctx); got != tt.want {
				t.Fatalf("userAgent() = %q, want %q", got, tt.want)
			}
			if !strings.Contains(tt.want, tt.contains) {
				t.Errorf("fallback = %q, want it to contain %q", tt.want, tt.contains)
			}
			if tt.provider != nil {
				got := tt.provider.(*stubUserAgent)
				if got.ctx != ctx {
					t.Errorf("provider context = %v, want caller context", got.ctx)
				}
			}
		})
	}
}
