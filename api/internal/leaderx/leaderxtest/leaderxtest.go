// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package leaderxtest holds what the tests of leader loops share.
package leaderxtest

import (
	"testing"
	"time"
)

// awaitBound is how long Await waits before it fails the test (HISS-02). A loop under test
// runs on a fake clock, so only a loop that never signals waits this long.
const awaitBound = 5 * time.Second

// Await blocks until ch delivers one signal and fails the test when none arrives in time.
// what names the awaited signal in the failure message.
func Await(tb testing.TB, ch <-chan struct{}, what string) {
	tb.Helper()
	select {
	case <-ch:
	case <-time.After(awaitBound):
		tb.Fatalf("timed out waiting for %s", what)
	}
}
