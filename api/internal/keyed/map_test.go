// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package keyed_test

import (
	"sync"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/keyed"
)

// workers bounds the concurrent fan-out of the race test (HISS-06).
const workers = 8

func TestMap(t *testing.T) {
	t.Parallel()
	type got struct {
		value string
		ok    bool
	}
	cases := []struct {
		name string
		do   func(m *keyed.Map[int, string])
		key  int
		want got
	}{
		// Positive.
		{"a stored value is returned", func(m *keyed.Map[int, string]) { m.Put(1, "a") }, 1, got{"a", true}},
		{"a second put replaces the first", func(m *keyed.Map[int, string]) { m.Put(1, "a"); m.Put(1, "b") }, 1, got{"b", true}},
		// Negative.
		{"another key is not found", func(m *keyed.Map[int, string]) { m.Put(1, "a") }, 2, got{}},
		{"a forgotten key is not found", func(m *keyed.Map[int, string]) { m.Put(1, "a"); m.Forget(1) }, 1, got{}},
		// Boundary.
		{"the zero value is empty", func(*keyed.Map[int, string]) {}, 1, got{}},
		{"forgetting on the zero value is a no-op", func(m *keyed.Map[int, string]) { m.Forget(1) }, 1, got{}},
		{"forgetting one key keeps the others", func(m *keyed.Map[int, string]) { m.Put(1, "a"); m.Put(2, "b"); m.Forget(1) }, 2, got{"b", true}},
		{"a key can be stored again after it was forgotten", func(m *keyed.Map[int, string]) { m.Put(1, "a"); m.Forget(1); m.Put(1, "c") }, 1, got{"c", true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var m keyed.Map[int, string]
			tc.do(&m)
			value, ok := m.Get(tc.key)
			if (got{value, ok}) != tc.want {
				t.Fatalf("Get(%d) = %q, %v; want %q, %v", tc.key, value, ok, tc.want.value, tc.want.ok)
			}
		})
	}
}

// Boundary: writers, readers and forgetters on the same keys do not race (run with -race).
func TestMapConcurrentUse(t *testing.T) {
	t.Parallel()
	var m keyed.Map[int, int]
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range 100 {
				m.Put(i%4, w)
				m.Get(i % 4)
				m.Forget((i + 1) % 4)
			}
		})
	}
	wg.Wait()
	m.Put(0, 7)
	if v, ok := m.Get(0); !ok || v != 7 {
		t.Fatalf("Get(0) = %d, %v after the concurrent phase; want 7, true", v, ok)
	}
}
