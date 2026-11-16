//go:build semgrep_fixture

package fixture

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/testkit/await"
	"github.com/lusoris/SnatchArr/api/internal/testkit/refuse"
)

// fatalAwaitInGoroutine documents a fatal-capable await from a goroutine.
func fatalAwaitInGoroutine(t *testing.T) {
	done := make(chan struct{})
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		await.Await(t, done, "goroutine completion")
	}()
}

// fatalListenerInGoroutine documents a fatal-capable listener from a goroutine.
func fatalListenerInGoroutine(t *testing.T) {
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		refuse.Listener(t)
	}()
}

// directFatalInGoroutine documents a direct fatal call from a goroutine.
func directFatalInGoroutine(t *testing.T) {
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		t.Fatalf("goroutine failed")
	}()
}

// awaitInTestGoroutine documents an await in the test goroutine.
func awaitInTestGoroutine(t *testing.T) {
	done := make(chan struct{})
	// ok: test-fatal-helper-in-goroutine
	await.Await(t, done, "test completion")
}

// errorInGoroutine documents a non-fatal report from a goroutine.
func errorInGoroutine(t *testing.T) {
	go func() {
		// ok: test-fatal-helper-in-goroutine
		t.Errorf("goroutine failed")
	}()
}

// awaitInCleanup documents an await in a cleanup callback.
func awaitInCleanup(t *testing.T) {
	done := make(chan struct{})
	t.Cleanup(func() {
		// ok: test-fatal-helper-in-goroutine
		await.Await(t, done, "cleanup completion")
	})
}
