// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"maps"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

// Main leaves the shared reaper running long enough to bridge the gaps between the package
// processes of one go test ./... run, and the database it starts serves New. Under -short
// Main starts neither, so there is nothing to check.
func TestMainLingersReaper(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test: needs Docker; run without -short")
	}
	if got := testcontainers.ReadConfig().Config.RyukReconnectionTimeout; got < time.Minute {
		t.Errorf("reaper reconnection timeout = %s, want at least 1m", got)
	}
	var one int
	if err := New(t).Pool().QueryRow(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Errorf("SELECT 1 = %d, %v", one, err)
	}
}

// lingerReaper fills in only an unset timeout. Not parallel: t.Setenv.
func TestLingerReaper(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"positive: unset gets the linger", nil, ryukLinger.String()},
		{"negative: the short name wins", map[string]string{ryukLingerEnv[0]: "30s"}, ""},
		{"boundary: a set long name is kept, however short", map[string]string{ryukLingerEnv[1]: "1s"}, "1s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range ryukLingerEnv {
				t.Setenv(name, tt.env[name])
			}
			if err := lingerReaper(); err != nil {
				t.Fatalf("lingerReaper: %v", err)
			}
			if got := os.Getenv(ryukLingerEnv[1]); got != tt.want {
				t.Errorf("%s = %q, want %q", ryukLingerEnv[1], got, tt.want)
			}
		})
	}
}

// execLocked bounds the statement, not the wait for the admin lock: a queue of parallel tests
// longer than the bound must not time out the statement at its head. Holding the lock only
// makes the package's other tests wait, which is what the bound must survive.
func TestExecLockedBoundsTheStatementNotTheQueue(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test: needs Docker; run without -short")
	}
	// The bound leaves SELECT 1 room at load 60; the held lock outlasts it.
	const bound = 3 * time.Second
	tests := []struct {
		name    string
		sql     string
		held    time.Duration
		wantErr bool
	}{
		{"positive: a lock held past the bound leaves the statement its bound", "SELECT 1", bound + 2*time.Second, false},
		{"negative: a statement longer than the bound times out", "SELECT pg_sleep(6)", 0, true},
		{"boundary: no wait, quick statement", "SELECT 1", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			shared.mu.Lock()
			done := make(chan error, 1)
			go func() { done <- execLocked(tt.sql, bound) }()
			time.Sleep(tt.held)
			shared.mu.Unlock()
			select {
			case err := <-done:
				if (err != nil) != tt.wantErr {
					t.Errorf("execLocked(%q) = %v, want error %v", tt.sql, err, tt.wantErr)
				}
			case <-time.After(30 * time.Second):
				t.Fatalf("execLocked(%q) did not return in 30s", tt.sql)
			}
		})
	}
}

// QuickInit adds initdb's --no-sync and keeps the environment other options set.
func TestQuickInit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{"an empty request gets the flag", nil, map[string]string{"POSTGRES_INITDB_ARGS": "--no-sync"}},
		{
			"other variables stay",
			map[string]string{"POSTGRES_USER": "test"},
			map[string]string{"POSTGRES_USER": "test", "POSTGRES_INITDB_ARGS": "--no-sync"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := testcontainers.GenericContainerRequest{Env: tc.env}
			if err := QuickInit()(&req); err != nil {
				t.Fatalf("QuickInit() = %v", err)
			}
			if !maps.Equal(req.Env, tc.want) {
				t.Errorf("Env = %v, want %v", req.Env, tc.want)
			}
		})
	}
}
