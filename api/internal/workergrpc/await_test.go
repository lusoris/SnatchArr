// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

// await returns after the poll interval for the head, never past the deadline, and at the
// deadline for a call behind the head.
func TestAwaitStopsAtDeadline(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		head     bool
		deadline time.Duration // from now
		want     time.Duration // when await returns
	}{
		{name: "head with a far deadline polls after the interval", head: true, deadline: 10 * time.Second, want: leasePollInterval},
		{name: "head with a near deadline returns at the deadline", head: true, deadline: 300 * time.Millisecond, want: 300 * time.Millisecond},
		{name: "head with the deadline one interval away", head: true, deadline: leasePollInterval, want: leasePollInterval},
		{name: "call behind the head waits for the deadline", deadline: 3 * time.Second, want: 3 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clockwork.NewFakeClock()
			s := &Server{clk: clk, stopping: make(chan struct{})}
			turn := s.line.join()
			if !tc.head {
				turn = s.line.join()
			}
			done := make(chan error, 1)
			go func() { done <- s.await(t.Context(), turn, clk.Now().Add(tc.deadline)) }()
			assertAwaitReturnsAt(t, clk, done, tc.want)
		})
	}
}

// assertAwaitReturnsAt checks that await's timer is still pending just before want and that
// await returns once the clock reaches want.
func assertAwaitReturnsAt(t *testing.T, clk *clockwork.FakeClock, done <-chan error, want time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := clk.BlockUntilContext(ctx, 1); err != nil {
		t.Fatalf("await set no timer: %v", err)
	}
	clk.Advance(want - time.Nanosecond)
	if err := clk.BlockUntilContext(ctx, 1); err != nil {
		t.Fatalf("await's timer fired before %v: %v", want, err)
	}
	clk.Advance(time.Nanosecond)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("await = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("await still waiting at %v", want)
	}
}
