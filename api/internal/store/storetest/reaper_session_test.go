// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

func TestOwnReaperSession(t *testing.T) {
	t.Parallel()
	setErr := errors.New("set failed")
	tests := []struct {
		name      string
		value     string
		present   bool
		generated string
		wantValue string
		setCalls  int
		generator int
		wantErr   error
	}{
		{name: "unset", generated: "generated-unset", wantValue: "generated-unset", setCalls: 1, generator: 1},
		{name: "empty", present: true, generated: "generated-empty", wantValue: "generated-empty", setCalls: 1, generator: 1},
		{name: "preset", value: "abc", present: true, setCalls: 0, generator: 0},
		{name: "set error", generated: "generated-error", wantValue: "generated-error", setCalls: 1, generator: 1, wantErr: setErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var setValue string
			setCalls := 0
			generatorCalls := 0
			err := ownReaperSession(
				func(key string) (string, bool) { return tt.value, tt.present },
				func(key, value string) error {
					setCalls++
					setValue = value
					return tt.wantErr
				},
				func() string {
					generatorCalls++
					return tt.generated
				},
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ownReaperSession() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if tt.wantErr != nil && !strings.Contains(err.Error(), reaperSessionEnv) {
				t.Errorf("ownReaperSession() error = %v, want %s", err, reaperSessionEnv)
			}
			if setCalls != tt.setCalls {
				t.Errorf("set calls = %d, want %d", setCalls, tt.setCalls)
			}
			if generatorCalls != tt.generator {
				t.Errorf("generator calls = %d, want %d", generatorCalls, tt.generator)
			}
			if setValue != tt.wantValue {
				t.Errorf("set value = %q, want %q", setValue, tt.wantValue)
			}
		})
	}
}

func TestOwnReaperSessionDefaultGenerator(t *testing.T) {
	t.Parallel()
	ids := make([]string, 0, 2)
	for range 2 {
		var id string
		if err := ownReaperSession(func(string) (string, bool) { return "", false }, func(_, value string) error {
			id = value
			return nil
		}, newReaperSessionID); err != nil {
			t.Fatalf("ownReaperSession() error = %v", err)
		}
		ids = append(ids, id)
	}
	if ids[0] == "" || ids[1] == "" {
		t.Fatalf("default generator returned empty ids: %q", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("default generator returned duplicate id %q", ids[0])
	}
}

func TestMainOwnsReaperSession(t *testing.T) {
	t.Parallel()
	id := os.Getenv(reaperSessionEnv)
	if id == "" {
		t.Fatal("TESTCONTAINERS_SESSION_ID is empty")
	}
	if got := testcontainers.SessionID(); got != id {
		t.Fatalf("testcontainers.SessionID() = %q, want %q", got, id)
	}
}
