//go:build semgrep_fixture

package fixture

import (
	"net/http"
	"time"
)

// testSubsecondClientTimeout covers sub-second client timeouts in both literal
// forms, second boundaries, and unrelated types with a Timeout field.
func testSubsecondClientTimeout() {
	// ruleid: test-subsecond-client-timeout
	_ = &http.Client{Timeout: 50 * time.Millisecond}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Millisecond * 50}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: 500 * time.Microsecond}
	// ruleid: test-subsecond-client-timeout
	_ = &http.Client{Timeout: time.Microsecond * 500}
	// ruleid: test-subsecond-client-timeout
	_ = &http.Client{Timeout: 100 * time.Nanosecond}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Nanosecond * 100}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Second / 2}
	// ruleid: test-subsecond-client-timeout
	_ = &http.Client{Timeout: time.Millisecond}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Microsecond}
	// ruleid: test-subsecond-client-timeout
	_ = &http.Client{Timeout: time.Nanosecond}

	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Second}
	// ok: test-subsecond-client-timeout
	_ = &http.Client{Timeout: 1000 * time.Millisecond}
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Millisecond * 1000}
	// ok: test-subsecond-client-timeout
	_ = &http.Client{Timeout: 1000000 * time.Microsecond}
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Microsecond * 1000000}
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: 1000000000 * time.Nanosecond}
	// ok: test-subsecond-client-timeout
	_ = &http.Client{Timeout: time.Nanosecond * 1000000000}
	// ok: test-subsecond-client-timeout
	_ = &http.Client{Timeout: time.Second / 1}
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: 5 * time.Second}

	type opts struct{ Timeout time.Duration }
	// ok: test-subsecond-client-timeout
	_ = opts{Timeout: 50 * time.Millisecond}
	// ok: test-subsecond-client-timeout
	_ = &opts{Timeout: time.Nanosecond}
}

func testSubsecondClientTimeoutAssignment(client *http.Client) {
	// ruleid: test-subsecond-client-timeout
	client.Timeout = 50 * time.Millisecond
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Millisecond * 50
	// ruleid: test-subsecond-client-timeout
	client.Timeout = 500 * time.Microsecond
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Microsecond * 500
	// ruleid: test-subsecond-client-timeout
	client.Timeout = 100 * time.Nanosecond
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Nanosecond * 100
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Second / 2
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Millisecond
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Microsecond
	// ruleid: test-subsecond-client-timeout
	client.Timeout = time.Nanosecond

	// ok: test-subsecond-client-timeout
	client.Timeout = time.Second
	// ok: test-subsecond-client-timeout
	client.Timeout = 1000 * time.Millisecond
	// ok: test-subsecond-client-timeout
	client.Timeout = 1000000 * time.Microsecond
	// ok: test-subsecond-client-timeout
	client.Timeout = 1000000000 * time.Nanosecond
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Second / 1

	type opts struct{ Timeout time.Duration }
	other := &opts{}
	// ok: test-subsecond-client-timeout
	other.Timeout = 50 * time.Millisecond
}
