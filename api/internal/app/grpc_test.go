// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"slices"
	"testing"
	"time"

	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
	grpcx "github.com/golusoris/golusoris/grpc"
)

// APP_GRPC_* reaches the gRPC server's keepalive keys through the app's config wiring, and
// an unset grace no longer cuts the worker's LeaseRun long-poll. Not parallel: t.Setenv.
func TestGRPCKeepaliveFromEnv(t *testing.T) {
	const (
		ageEnv   = "APP_GRPC_KEEPALIVE_MAX_CONNECTION_AGE"
		graceEnv = "APP_GRPC_KEEPALIVE_MAX_CONNECTION_AGE_GRACE"
	)
	tests := []struct {
		name      string
		env       map[string]string
		wantAge   time.Duration
		wantGrace time.Duration
		wantErr   bool
	}{
		{"nothing set", nil, 2 * time.Minute, grpcx.Infinite, false},
		{"grace from env", map[string]string{graceEnv: "45s"}, 2 * time.Minute, 45 * time.Second, false},
		{"age from env", map[string]string{ageEnv: "10m"}, 10 * time.Minute, grpcx.Infinite, false},
		{"both from env", map[string]string{ageEnv: "1h", graceEnv: "-1s"}, time.Hour, -time.Second, false},
		{"short grace from env refused", map[string]string{graceEnv: "10s"}, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			var got grpcx.Config
			app := fx.New(fx.NopLogger, gconfig.Module, grpcx.Module, grpcConfig, fx.Populate(&got))
			if err := app.Err(); (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Keepalive.MaxConnectionAge != tt.wantAge || got.Keepalive.MaxConnectionAgeGrace != tt.wantGrace {
				t.Errorf("age, grace = %s, %s; want %s, %s", got.Keepalive.MaxConnectionAge,
					got.Keepalive.MaxConnectionAgeGrace, tt.wantAge, tt.wantGrace)
			}
		})
	}
}

// The decorator keeps compound keys the app already declares.
func TestWithCompoundKeysKeepsExisting(t *testing.T) {
	t.Parallel()
	got := withCompoundKeys(gconfig.Options{EnvPrefix: "APP_", CompoundKeys: []string{"search.api_key"}})
	if !slices.Contains(got.CompoundKeys, "search.api_key") {
		t.Errorf("existing key dropped: %v", got.CompoundKeys)
	}
	if !slices.Contains(got.CompoundKeys, "grpc.keepalive.max_connection_age_grace") {
		t.Errorf("grpc grace key missing: %v", got.CompoundKeys)
	}
	if !slices.Contains(got.CompoundKeys, "snatcharr.proxy_auth.enabled") {
		t.Errorf("snatcharr proxy auth key missing: %v", got.CompoundKeys)
	}
	if !slices.Contains(got.CompoundKeys, "snatcharr.oidc.issuer_url") {
		t.Errorf("snatcharr oidc issuer key missing: %v", got.CompoundKeys)
	}
	if got.EnvPrefix != "APP_" {
		t.Errorf("EnvPrefix = %q, want APP_", got.EnvPrefix)
	}
}
