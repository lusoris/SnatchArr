// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
	grpcx "github.com/golusoris/golusoris/grpc"
)

// keepaliveFrom loads yaml as the app would and returns the grpc.Config a consumer module
// sees with LeaseKeepalive decorating at the root.
func keepaliveFrom(t *testing.T, yaml string) (grpcx.KeepaliveConfig, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := gconfig.New(gconfig.Options{Files: []string{file}, EnvPrefix: "SNATCHARR_KEEPALIVE_TEST_UNSET_"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	var got grpcx.Config
	app := fx.New(fx.NopLogger, fx.Supply(cfg), grpcx.Module, fx.Decorate(LeaseKeepalive),
		fx.Module("consumer", fx.Invoke(func(c grpcx.Config) { got = c })))
	return got.Keepalive, app.Err()
}

func TestLeaseKeepalive(t *testing.T) {
	t.Parallel()
	atMin := minConnectionAgeGrace.String()
	belowMin := (minConnectionAgeGrace - time.Nanosecond).String()
	tests := []struct {
		name      string
		keepalive string
		wantGrace time.Duration
		wantErr   error
	}{
		{"unset grace becomes unlimited", "{}", grpcx.Infinite, nil},
		{"unset grace, age set", "{max_connection_age: 10m}", grpcx.Infinite, nil},
		{"long grace kept", "{max_connection_age_grace: 45s}", 45 * time.Second, nil},
		{"grace at the minimum kept", "{max_connection_age_grace: " + atMin + "}", minConnectionAgeGrace, nil},
		{"negative grace (unlimited) kept", "{max_connection_age_grace: -1s}", -time.Second, nil},
		{"short grace kept when age never rotates", "{max_connection_age: -1s, max_connection_age_grace: 5s}", 5 * time.Second, nil},
		{"grace one ns below the minimum refused", "{max_connection_age_grace: " + belowMin + "}", 0, errGraceTooShort},
		{"golusoris default grace refused", "{max_connection_age_grace: 5s}", 0, errGraceTooShort},
		{"zero grace (golusoris default) refused", "{max_connection_age_grace: 0s}", 0, errGraceTooShort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := keepaliveFrom(t, "grpc:\n  keepalive: "+tt.keepalive+"\n")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && got.MaxConnectionAgeGrace != tt.wantGrace {
				t.Errorf("grace = %s, want %s", got.MaxConnectionAgeGrace, tt.wantGrace)
			}
		})
	}
}

// A finite grace that passes still outlasts the longest LeaseRun long-poll.
func TestMinConnectionAgeGraceCoversLeaseWait(t *testing.T) {
	t.Parallel()
	if minConnectionAgeGrace <= maxLeaseWait+leasePollInterval {
		t.Fatalf("minConnectionAgeGrace %s does not cover maxLeaseWait %s plus one poll", minConnectionAgeGrace, maxLeaseWait)
	}
}
