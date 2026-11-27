// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"errors"
	"log/slog"
	"syscall"
	"testing"

	"code.dny.dev/ssrf"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

func allowAllDialControl(string, string, syscall.RawConn) error {
	return nil
}

func newDialControlTestService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	configOpts := config.DefaultOptions()
	configOpts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	compiled, err := config.Compile(configOpts)
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	rec, err := metrics.New(compiled, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	svc, err := New(compiled, rec, clockwork.NewRealClock(), slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	return svc
}

func TestNewDialControlDefaultGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts []Option
	}{
		{name: "no option"},
		{name: "nil option", opts: []Option{WithDialControl(nil)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newDialControlTestService(t, tc.opts...)
			if svc.dialControl == nil {
				t.Fatal("dial control is nil")
			}
			err := svc.dialControl("tcp4", "127.0.0.1:443", nil)
			if !errors.Is(err, ssrf.ErrProhibitedIP) {
				t.Fatalf("dial control error = %v, want errors.Is(_, %v)", err, ssrf.ErrProhibitedIP)
			}
		})
	}
}

func TestNewDialControlUsesCustomControl(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("custom dial control")
	called := false
	custom := func(string, string, syscall.RawConn) error {
		called = true
		return wantErr
	}
	svc := newDialControlTestService(t, WithDialControl(custom))
	if err := svc.dialControl("tcp4", "127.0.0.1:443", nil); !errors.Is(err, wantErr) {
		t.Fatalf("dial control error = %v, want %v", err, wantErr)
	}
	if !called {
		t.Fatal("custom dial control was not called")
	}
}

func TestWithDialControlNilOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts func(DialControl) []Option
	}{
		{
			name: "nil after custom keeps custom",
			opts: func(custom DialControl) []Option {
				return []Option{WithDialControl(custom), WithDialControl(nil)}
			},
		},
		{
			name: "custom after nil uses custom",
			opts: func(custom DialControl) []Option {
				return []Option{WithDialControl(nil), WithDialControl(custom)}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wantErr := errors.New("ordered custom dial control")
			called := false
			custom := func(string, string, syscall.RawConn) error {
				called = true
				return wantErr
			}
			svc := newDialControlTestService(t, tc.opts(custom)...)
			if svc.dialControl == nil {
				t.Fatal("dial control is nil")
			}
			if err := svc.dialControl("tcp4", "127.0.0.1:443", nil); !errors.Is(err, wantErr) {
				t.Fatalf("dial control error = %v, want %v", err, wantErr)
			}
			if !called {
				t.Fatal("custom dial control was not called")
			}
		})
	}
}
