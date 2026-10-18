// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// maxHeldConns bounds the connections a silent listener accepts (HISS-02); one observation
// opens a few at most.
const maxHeldConns = 64

// silentListener accepts connections and never answers, like a client that hangs.
func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for range maxHeldConns {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		if cerr := ln.Close(); cerr != nil {
			t.Errorf("close listener: %v", cerr)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			if cerr := c.Close(); cerr != nil {
				t.Errorf("close conn: %v", cerr)
			}
		}
	})
	return "http://" + ln.Addr().String()
}

// A client that accepts the connection and never answers must end as an unreachable snapshot
// within the observation bound, not block the snapshotter.
func TestSnapshot_ClientThatNeverAnswers(t *testing.T) {
	t.Parallel()
	const bound = 300 * time.Millisecond
	cases := []struct {
		name    string
		timeout time.Duration
	}{
		// Positive: the bound ends the hang.
		{name: "bound", timeout: bound},
		// Boundary: the smallest bound still yields a snapshot with its cause.
		{name: "one millisecond", timeout: time.Millisecond},
	}
	for _, tc := range cases {
		for _, kind := range domain.AllDownloadClientKinds {
			t.Run(tc.name+"/"+string(kind), func(t *testing.T) {
				t.Parallel()
				g := dlclients.NewGoenvoyWithTimeout(clock.NewFake(), tc.timeout)
				c := domain.DownloadClient{Kind: kind, Name: "c", BaseURL: silentListener(t), Username: "u", Enabled: true}
				done := make(chan dlclients.Snapshot, 1)
				go func() { done <- g.Snapshot(context.Background(), c, "p") }()
				select {
				case s := <-done:
					// Negative: a hang is never reported as reachable.
					if s.Reachable || s.Error == "" || s.Kind != kind {
						t.Fatalf("got %+v, want an unreachable snapshot with its cause", s)
					}
				case <-time.After(tc.timeout + 5*time.Second):
					t.Fatalf("snapshot still blocked %v after a %v bound", 5*time.Second, tc.timeout)
				}
			})
		}
	}
}
