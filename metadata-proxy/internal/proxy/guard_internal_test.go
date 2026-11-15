// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"errors"
	"testing"

	"code.dny.dev/ssrf"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
)

func compileGuardForTest(t *testing.T, deniedPrefixes []string) DialControl {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.Ports = []int{443}
	opts.DeniedPrefixes = deniedPrefixes
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("config.Compile(%v): %v", deniedPrefixes, err)
	}
	return buildGuardianControl(compiled)
}

func TestGuardCustomDeniedPrefixes(t *testing.T) {
	t.Parallel()

	specs := []struct {
		name     string
		network  string
		prefix   string
		target   string
		first    string
		last     string
		pastLast string
	}{
		{
			name:     "IPv4 100.128.0.0/16",
			network:  "tcp4",
			prefix:   "100.128.0.0/16",
			target:   "100.128.0.7:443",
			first:    "100.128.0.0:443",
			last:     "100.128.255.255:443",
			pastLast: "100.129.0.0:443",
		},
		{
			name:     "IPv6 2c0f:fff0::/32",
			network:  "tcp6",
			prefix:   "2c0f:fff0::/32",
			target:   "[2c0f:fff0::7]:443",
			first:    "[2c0f:fff0::]:443",
			last:     "[2c0f:fff0:ffff:ffff:ffff:ffff:ffff:ffff]:443",
			pastLast: "[2c0f:fff1::]:443",
		},
	}

	for _, spec := range specs {
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()

			defaultGuard := compileGuardForTest(t, nil)
			customGuard := compileGuardForTest(t, []string{spec.prefix})

			cases := []struct {
				name    string
				guard   DialControl
				addr    string
				wantErr error
			}{
				{
					name:    "independence: allowed without denied_prefixes",
					guard:   defaultGuard,
					addr:    spec.target,
					wantErr: nil,
				},
				{
					name:    "negative: target inside denied_prefixes refused",
					guard:   customGuard,
					addr:    spec.target,
					wantErr: ssrf.ErrProhibitedIP,
				},
				{
					name:    "boundary: first address of prefix refused",
					guard:   customGuard,
					addr:    spec.first,
					wantErr: ssrf.ErrProhibitedIP,
				},
				{
					name:    "boundary: last address of prefix refused",
					guard:   customGuard,
					addr:    spec.last,
					wantErr: ssrf.ErrProhibitedIP,
				},
				{
					name:    "boundary: address one past last allowed",
					guard:   customGuard,
					addr:    spec.pastLast,
					wantErr: nil,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					err := tc.guard(spec.network, tc.addr, nil)
					if tc.wantErr == nil {
						if err != nil {
							t.Fatalf("guard(%s, %s) = %v, want nil", spec.network, tc.addr, err)
						}
						return
					}
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("guard(%s, %s) = %v, want errors.Is(_, %v)", spec.network, tc.addr, err, tc.wantErr)
					}
				})
			}
		})
	}
}
