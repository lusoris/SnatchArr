// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"maps"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

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
