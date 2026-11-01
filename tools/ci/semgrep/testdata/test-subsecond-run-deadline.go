//go:build semgrep_fixture

package fixture

import (
	"context"
	"testing"
	"time"
)

func testSubsecondRunDeadline(t *testing.T) {
	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 50*time.Millisecond)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 500*time.Microsecond)

	// ruleid: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 100*time.Nanosecond)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 10*time.Second)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 1000*time.Millisecond)

	// ok: test-subsecond-run-deadline
	_, _ = context.WithTimeout(t.Context(), 5*time.Second)
}
