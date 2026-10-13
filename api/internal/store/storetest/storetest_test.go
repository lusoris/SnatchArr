// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

// Main leaves the shared reaper running long enough to bridge the gaps between the package
// processes of one go test ./... run, and the database it starts serves New.
func TestMainLingersReaper(t *testing.T) {
	t.Parallel()
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
