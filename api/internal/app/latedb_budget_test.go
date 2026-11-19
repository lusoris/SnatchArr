// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"

	dbpgx "github.com/golusoris/golusoris/db/pgx"
)

var errLateDBStartBudget = errors.New("late database start budget is too small")

// lateDBConnectAttemptCase specifies one pool connection budget calculation.
type lateDBConnectAttemptCase struct {
	name                      string
	remaining, connectTimeout time.Duration
	retryMax                  time.Duration
	cap                       int
	customParameters          bool
	want                      int
	wantErr                   bool
}

// lateDBProxyPollCase specifies one listener polling budget calculation.
type lateDBProxyPollCase struct {
	name                    string
	remaining, pollInterval time.Duration
	cap                     int
	customParameters        bool
	want                    int
}

// lateDBConnectAttempts bounds pool connection work to the remaining app start budget.
func lateDBConnectAttempts(remaining, connectTimeout, retryMax time.Duration, upperCap int) (int, error) {
	perAttempt := connectTimeout + retryMax
	if perAttempt <= 0 || remaining < perAttempt || upperCap < 1 {
		return 0, fmt.Errorf("%w: remaining %s, per attempt %s", errLateDBStartBudget, remaining, perAttempt)
	}
	attempts := min(int(remaining/perAttempt), upperCap)
	return attempts, nil
}

// lateDBProxyPollCount bounds listener polling to the remaining app start budget.
func lateDBProxyPollCount(remaining, pollInterval time.Duration, upperCap int) int {
	if pollInterval <= 0 || upperCap < 1 {
		return 0
	}
	if remaining <= 0 {
		return 1
	}
	polls := remaining / pollInterval
	if polls >= time.Duration(upperCap) {
		return upperCap
	}
	if remaining%pollInterval != 0 {
		polls++
	}
	polls++
	if polls > time.Duration(upperCap) {
		return upperCap
	}
	return int(polls)
}

// TestLateDBConnectAttempts covers exhausted, exact-attempt, capped, and ordinary budgets.
func TestLateDBConnectAttempts(t *testing.T) {
	t.Parallel()
	perAttempt := lateDBConnectTime + lateDBRetryMax
	tests := []lateDBConnectAttemptCase{
		{name: "zero remaining", remaining: 0, wantErr: true},
		{name: "negative remaining", remaining: -time.Nanosecond, wantErr: true},
		{name: "below one attempt", remaining: perAttempt - time.Nanosecond, wantErr: true},
		{name: "zero upper cap", remaining: perAttempt, connectTimeout: lateDBConnectTime, retryMax: lateDBRetryMax, customParameters: true, cap: 0, wantErr: true},
		{name: "zero duration attempt", remaining: perAttempt, customParameters: true, cap: maxLateDBAttempts, wantErr: true},
		{name: "negative duration attempt", remaining: perAttempt, connectTimeout: -time.Nanosecond, customParameters: true, cap: maxLateDBAttempts, wantErr: true},
		{name: "exactly one attempt", remaining: perAttempt, want: 1},
		{name: "upper cap", remaining: time.Duration(maxLateDBAttempts+1) * perAttempt, want: maxLateDBAttempts},
		{name: "thirty seconds", remaining: 30 * time.Second, want: 24},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			connectTimeout, retryMax, upperCap := lateDBAttemptParameters(test)
			got, err := lateDBConnectAttempts(test.remaining, connectTimeout, retryMax, upperCap)
			if test.wantErr {
				if !errors.Is(err, errLateDBStartBudget) {
					t.Fatalf("lateDBConnectAttempts error = %v, want %v", err, errLateDBStartBudget)
				}
				return
			}
			if err != nil {
				t.Fatalf("lateDBConnectAttempts error = %v", err)
			}
			if got != test.want {
				t.Errorf("lateDBConnectAttempts = %d, want %d", got, test.want)
			}
			if time.Duration(got)*(connectTimeout+retryMax) > test.remaining {
				t.Errorf("attempt budget %s exceeds remaining %s", time.Duration(got)*perAttempt, test.remaining)
			}
		})
	}
}

// lateDBAttemptParameters supplies default retry limits unless a row requests literals.
func lateDBAttemptParameters(test lateDBConnectAttemptCase) (time.Duration, time.Duration, int) {
	if test.customParameters {
		return test.connectTimeout, test.retryMax, test.cap
	}
	return lateDBConnectTime, lateDBRetryMax, maxLateDBAttempts
}

// TestLateDBProxyPollCount covers rounded deadlines, exact intervals, and the scalar cap.
func TestLateDBProxyPollCount(t *testing.T) {
	t.Parallel()
	tests := []lateDBProxyPollCase{
		{name: "zero remaining", remaining: 0, want: 1},
		{name: "negative remaining", remaining: -time.Nanosecond, want: 1},
		{name: "zero poll interval", remaining: proxyPollInterval, customParameters: true, cap: maxProxyPollCount, want: 0},
		{name: "zero upper cap", remaining: proxyPollInterval, pollInterval: proxyPollInterval, customParameters: true, cap: 0, want: 0},
		{name: "exact multiple", remaining: 2 * proxyPollInterval, want: 3},
		{name: "rounded up", remaining: 2*proxyPollInterval + time.Nanosecond, want: 4},
		{name: "one below upper cap", remaining: time.Duration(maxProxyPollCount-2) * proxyPollInterval, want: maxProxyPollCount - 1},
		{name: "upper cap", remaining: time.Duration(maxProxyPollCount) * proxyPollInterval, want: maxProxyPollCount},
		{name: "thirty seconds", remaining: 30 * time.Second, want: 121},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pollInterval, upperCap := lateDBProxyParameters(test)
			got := lateDBProxyPollCount(test.remaining, pollInterval, upperCap)
			if got != test.want {
				t.Errorf("lateDBProxyPollCount(%s) = %d, want %d", test.remaining, got, test.want)
			}
		})
	}
}

// lateDBProxyParameters supplies default polling limits unless a row requests literals.
func lateDBProxyParameters(test lateDBProxyPollCase) (time.Duration, int) {
	if test.customParameters {
		return test.pollInterval, test.cap
	}
	return proxyPollInterval, maxProxyPollCount
}

// TestLateDatabaseConfigBudget verifies every pool setting derives from the start budget.
func TestLateDatabaseConfigBudget(t *testing.T) {
	t.Parallel()
	perAttempt := lateDBConnectTime + lateDBRetryMax
	ctx, cancel := context.WithTimeout(t.Context(), 12*perAttempt+perAttempt/2)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("late database config context has no deadline")
	}
	before := deadline.Sub(clockwork.NewRealClock().Now())
	cfg := lateDatabaseConfig(
		t,
		ctx,
		testDatabase{user: "test", password: "test", host: "127.0.0.1:1", name: "test"},
		"127.0.0.1:2",
		"127.0.0.1:3",
	)
	after := deadline.Sub(clockwork.NewRealClock().Now())
	opts := dbpgx.DefaultOptions()
	if err := cfg.Unmarshal("db", &opts); err != nil {
		t.Fatalf("unmarshal database options: %v", err)
	}
	if opts.ConnectTimeout != lateDBConnectTime || opts.Retry.Max != lateDBRetryMax || opts.Retry.Initial != lateDBRetryInitial {
		t.Errorf("database timeouts = connect %s, initial %s, max %s", opts.ConnectTimeout, opts.Retry.Initial, opts.Retry.Max)
	}
	if opts.Retry.Attempts < int(after/perAttempt) || opts.Retry.Attempts > int(before/perAttempt) {
		t.Errorf("database retry attempts = %d, want between %d and %d", opts.Retry.Attempts, int(after/perAttempt), int(before/perAttempt))
	}
	if time.Duration(opts.Retry.Attempts)*(opts.ConnectTimeout+opts.Retry.Max) > before {
		t.Errorf("database retry budget exceeds remaining %s", before)
	}
}

// TestLateDatabaseProxyPollLimit proves timed-out polls reach their own bound.
func TestLateDatabaseProxyPollLimit(t *testing.T) {
	t.Parallel()
	listener := lateDatabaseTestListener(t)
	const pollLimit = 2
	stats := &lateDatabaseProxyStats{}
	// Serve gets no deadline because each poll has its own listener deadline and the loop has a scalar bound.
	err := serveLateDatabaseProxy(t, t.Context(), listener, "127.0.0.1:1", lateDBDelay, 1, pollLimit, stats)
	want := fmt.Sprintf("proxy accept loop reached %d polls", pollLimit)
	if err == nil || err.Error() != want {
		t.Errorf("serveLateDatabaseProxy error = %v, want %q", err, want)
	}
	if got := stats.polls.Load(); got != pollLimit {
		t.Errorf("proxy timed-out polls = %d, want %d", got, pollLimit)
	}
}

// TestLateDatabaseProxyPollLimitAfterAccepts proves queued accepts spend no poll budget.
func TestLateDatabaseProxyPollLimitAfterAccepts(t *testing.T) {
	t.Parallel()
	listener := lateDatabaseTestListener(t)
	ctx, cancel := context.WithTimeout(t.Context(), lateDBTimeout)
	defer cancel()
	const accepted, pollLimit = 3, 2
	for range accepted {
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("queue late database connection: %v", err)
		}
		if err := conn.SetDeadline(clockwork.NewRealClock().Now().Add(lateDBTimeout)); err != nil {
			t.Fatalf("deadline on queued connection: %v", err)
		}
		t.Cleanup(func() { closeProxyConnection(t, conn) })
	}
	stats := &lateDatabaseProxyStats{}
	// Accepted connections take their I/O deadline from ctx, and lateDBTimeout is far above the expected run of two polls.
	err := serveLateDatabaseProxy(t, ctx, listener, "127.0.0.1:1", lateDBTimeout, 4, pollLimit, stats)
	want := fmt.Sprintf("proxy accept loop reached %d polls", pollLimit)
	if err == nil || err.Error() != want {
		t.Errorf("serveLateDatabaseProxy error = %v, want %q", err, want)
	}
	if got := stats.polls.Load(); got != pollLimit {
		t.Errorf("proxy timed-out polls = %d, want %d", got, pollLimit)
	}
	if got := stats.rejected.Load(); got != accepted {
		t.Errorf("proxy rejected connections = %d, want %d", got, accepted)
	}
}

// lateDatabaseTestListener binds a proxy listener and closes it at test cleanup.
func lateDatabaseTestListener(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen for late database proxy: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close late database proxy listener: %v", closeErr)
		}
	})
	return listener
}
