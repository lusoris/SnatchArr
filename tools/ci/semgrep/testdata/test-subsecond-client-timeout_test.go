//go:build semgrep_fixture

package fixture

import (
	"net/http"
	nethttp "net/http"
	"time"
	clock "time"
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

// testSubsecondClientTimeoutAssignment covers pointer writes and unrelated fields.
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
	var value opts
	// ok: test-subsecond-client-timeout
	value.Timeout = 50 * time.Millisecond
}

// testClientTimeoutAliases covers independently aliased imports and typed writes.
func testClientTimeoutAliases(client *nethttp.Client) {
	// ruleid: test-subsecond-client-timeout
	_ = nethttp.Client{Timeout: 50 * time.Millisecond}
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: 50 * clock.Millisecond}
	// ruleid: test-subsecond-client-timeout
	_ = nethttp.Client{Timeout: 50 * clock.Millisecond}
	// ruleid: test-subsecond-client-timeout
	client.Timeout = 50 * clock.Millisecond
	var value nethttp.Client
	// ruleid: test-subsecond-client-timeout
	value.Timeout = clock.Millisecond * 50
}

// packageTimeout covers a package-level duration constant.
const packageTimeout = 50 * time.Millisecond

// packageVariableTimeout covers a mutable package-level duration.
var packageVariableTimeout = 50 * time.Millisecond

// testNamedClientTimeouts covers local and package duration expressions.
func testNamedClientTimeouts(client *http.Client) {
	d := 50 * time.Millisecond
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: d}
	// ruleid: test-subsecond-client-timeout
	client.Timeout = d
	const localTimeout = 50 * time.Millisecond
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: localTimeout}
	// ruleid: test-subsecond-client-timeout
	client.Timeout = localTimeout
	// ruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: packageTimeout}
	// Semgrep CE does not propagate this package constant into a field assignment.
	// todoruleid: test-subsecond-client-timeout
	client.Timeout = packageTimeout
	// Semgrep CE does not propagate mutable package durations into literals.
	// todoruleid: test-subsecond-client-timeout
	_ = http.Client{Timeout: packageVariableTimeout}
	// Semgrep CE does not propagate mutable package durations into field assignments.
	// todoruleid: test-subsecond-client-timeout
	client.Timeout = packageVariableTimeout
}

// testValueClientTimeouts covers writes to a non-pointer HTTP client.
func testValueClientTimeouts() {
	var client http.Client
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
}

// testDisabledMillisecondTimeouts covers nonpositive multipliers in either order.
func testDisabledMillisecondTimeouts(client *http.Client) {
	var value http.Client
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: 0 * time.Millisecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = 0 * time.Millisecond
	// ok: test-subsecond-client-timeout
	value.Timeout = 0 * time.Millisecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Millisecond * 0}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Millisecond * 0
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Millisecond * 0
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: -1 * time.Millisecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = -1 * time.Millisecond
	// ok: test-subsecond-client-timeout
	value.Timeout = -1 * time.Millisecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Millisecond * -1}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Millisecond * -1
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Millisecond * -1
}

// testDisabledMicrosecondTimeouts covers nonpositive multipliers in either order.
func testDisabledMicrosecondTimeouts(client *http.Client) {
	var value http.Client
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: 0 * time.Microsecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = 0 * time.Microsecond
	// ok: test-subsecond-client-timeout
	value.Timeout = 0 * time.Microsecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Microsecond * 0}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Microsecond * 0
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Microsecond * 0
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: -1 * time.Microsecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = -1 * time.Microsecond
	// ok: test-subsecond-client-timeout
	value.Timeout = -1 * time.Microsecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Microsecond * -1}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Microsecond * -1
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Microsecond * -1
}

// testDisabledNanosecondTimeouts covers nonpositive multipliers in either order.
func testDisabledNanosecondTimeouts(client *http.Client) {
	var value http.Client
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: 0 * time.Nanosecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = 0 * time.Nanosecond
	// ok: test-subsecond-client-timeout
	value.Timeout = 0 * time.Nanosecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Nanosecond * 0}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Nanosecond * 0
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Nanosecond * 0
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: -1 * time.Nanosecond}
	// ok: test-subsecond-client-timeout
	client.Timeout = -1 * time.Nanosecond
	// ok: test-subsecond-client-timeout
	value.Timeout = -1 * time.Nanosecond
	// ok: test-subsecond-client-timeout
	_ = http.Client{Timeout: time.Nanosecond * -1}
	// ok: test-subsecond-client-timeout
	client.Timeout = time.Nanosecond * -1
	// ok: test-subsecond-client-timeout
	value.Timeout = time.Nanosecond * -1
}
