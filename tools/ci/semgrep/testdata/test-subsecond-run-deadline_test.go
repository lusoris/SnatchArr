//go:build semgrep_fixture

package fixture

import (
	"context"
	"testing"
	"time"
)

// testSubsecondRunDeadline holds the calls the test-subsecond-run-deadline
// rule must refuse (sub-second deadlines over t.Context()) and the calls it
// must allow (deadlines of one second or more).
func testSubsecondRunDeadline(t *testing.T) {
	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 50*time.Millisecond)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 500*time.Microsecond)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 100*time.Nanosecond)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), time.Millisecond*50)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), time.Second/2)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 10*time.Second)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 1000000*time.Microsecond)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 2000000000*time.Nanosecond)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), time.Second/1)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 1000*time.Millisecond)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 5*time.Second)
}
