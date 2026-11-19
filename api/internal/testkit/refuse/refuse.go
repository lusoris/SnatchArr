// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package refuse keeps a loopback port bound while refusing every connection.
// A closed test server's port can be rebound by parallel servers (issue 1193).
package refuse

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/testkit/await"
)

const (
	maxConnections = 256
	listenBound    = 5 * time.Second
	// listenerBound ends the accept loop while cleanup keeps the address reserved.
	listenerBound = 20 * time.Minute
)

// Listener reserves a loopback port until cleanup so parallel test servers
// cannot rebind the refused address from issue 1193.
func Listener(tb testing.TB) string {
	tb.Helper()
	url, _ := listener(tb, listenerBound)
	return url
}

// listener exposes the accept-loop signal for the lifetime-bound test. Its
// lifetime bound ends the accept loop while cleanup keeps the address reserved.
func listener(tb testing.TB, lifetime time.Duration) (string, <-chan struct{}) {
	tb.Helper()
	listenCtx, cancelListen := context.WithTimeout(tb.Context(), listenBound)
	defer cancelListen()
	ln, err := (&net.ListenConfig{}).Listen(listenCtx, "tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen for refused requests: %v", err)
	}
	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		closeListener(tb, ln, "after unexpected listener type")
		tb.Fatalf("refusing listener has type %T, want *net.TCPListener", ln)
	}

	lifetimeCtx, cancelLifetime := context.WithTimeout(tb.Context(), lifetime)
	acceptDone := make(chan struct{})
	closeDone := make(chan struct{})
	stopClose := context.AfterFunc(lifetimeCtx, func() {
		defer close(closeDone)
		if err := tcpLn.SetDeadline(time.Unix(1, 0)); err != nil && !errors.Is(err, net.ErrClosed) {
			tb.Errorf("end refusing listener accept loop: %v", err)
		}
	})
	go refuseConnections(tb, ln, acceptDone)

	tb.Cleanup(func() {
		stopped := stopClose()
		cancelLifetime()
		if !stopped {
			await.Await(tb, closeDone, "refusing listener lifetime closer")
		}
		closeListener(tb, ln, "during cleanup")
		await.Await(tb, acceptDone, "refusing listener accept loop")
	})
	return "http://" + ln.Addr().String(), acceptDone
}

// refuseConnections closes accepted connections at once. A lifetime bound ends
// the accept loop while cleanup keeps the listener bound for issue 1193.
func refuseConnections(tb testing.TB, ln net.Listener, done chan<- struct{}) {
	tb.Helper()
	defer close(done)
	for range maxConnections {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) {
			return
		}
		if err != nil {
			tb.Errorf("accept refused request: %v", err)
			return
		}
		if err := conn.Close(); err != nil {
			tb.Errorf("close refused request: %v", err)
			return
		}
	}
	tb.Errorf("refused more than %d connections", maxConnections)
}

// closeListener reports unexpected close failures during listener cleanup.
func closeListener(tb testing.TB, ln net.Listener, when string) {
	tb.Helper()
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		tb.Errorf("close refusing listener %s: %v", when, err)
	}
}
