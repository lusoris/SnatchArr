// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package refuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/testkit/await"
)

// TestListener_RequestFails checks that the issue 1193 listener refuses a request.
func TestListener_RequestFails(t *testing.T) {
	t.Parallel()
	url := Listener(t)
	assertRequestRefused(t, url)
}

// TestListener_PortRemainsBound checks that issue 1193 cannot recur through a
// parallel listener rebinding the refused address.
func TestListener_PortRemainsBound(t *testing.T) {
	t.Parallel()
	url := Listener(t)
	assertAddressRemainsBound(t, strings.TrimPrefix(url, "http://"))
}

// TestListener_BoundaryMinusOneRequestsFail checks the bounded accept loop
// continues refusing through its supported issue 1193 request boundary.
func TestListener_BoundaryMinusOneRequestsFail(t *testing.T) {
	t.Parallel()
	url := Listener(t)
	for range maxConnections - 1 {
		assertRequestRefused(t, url)
	}
}

// TestListener_LifetimeEndsAcceptLoop checks the issue 1193 listener ends its
// accept loop at the lifetime bound while retaining its reserved address.
func TestListener_LifetimeEndsAcceptLoop(t *testing.T) {
	t.Parallel()
	url, done := listener(t, 10*time.Millisecond)
	await.Await(t, done, "lifetime-bounded refusing listener")
	assertAddressRemainsBound(t, strings.TrimPrefix(url, "http://"))
}

// TestRefusal_HeldConnectionFails checks that a connection held open after
// acceptance is not mistaken for a refused endpoint.
func TestRefusal_HeldConnectionFails(t *testing.T) {
	t.Parallel()
	ln := openTestListener(t)
	accepted := make(chan struct{})
	release := make(chan struct{})
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept held refusal connection: %v", err)
			return
		}
		defer func() {
			if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
				t.Errorf("close held refusal connection: %v", closeErr)
			}
		}()
		close(accepted)
		await.Await(t, release, "held refusal connection release")
	}()
	t.Cleanup(func() {
		close(release)
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close held refusal listener: %v", closeErr)
		}
		await.Await(t, acceptDone, "held refusal accept loop")
	})

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	refusalDone := make(chan struct{})
	go func() {
		defer close(refusalDone)
		result <- refusal(ctx, ln.Addr().String())
	}()
	await.Await(t, accepted, "held refusal connection acceptance")
	cancel()
	await.Await(t, refusalDone, "held refusal check")
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("held connection refusal error = %v, want context.Canceled", err)
	}
}

// TestRefusal_DataSentFails checks that endpoint data is not mistaken for a
// refused connection.
func TestRefusal_DataSentFails(t *testing.T) {
	t.Parallel()
	ln := openTestListener(t)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept data refusal connection: %v", err)
			return
		}
		if _, writeErr := conn.Write([]byte{'x'}); writeErr != nil {
			t.Errorf("write refusal data: %v", writeErr)
		}
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close data refusal connection: %v", closeErr)
		}
	}()
	t.Cleanup(func() {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close data refusal listener: %v", closeErr)
		}
		await.Await(t, acceptDone, "data refusal accept loop")
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := refusal(ctx, ln.Addr().String())
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("data refusal error = %v, want non-context error", err)
	}
}

// assertAddressRemainsBound fails when a second listener binds the refused address.
func assertAddressRemainsBound(tb testing.TB, addr string) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err == nil {
		if closeErr := ln.Close(); closeErr != nil {
			tb.Errorf("close unexpected listener: %v", closeErr)
		}
		tb.Fatal("another listener bound the refused port")
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		tb.Fatalf("rebind failed for another reason: %v", err)
	}
}

// openTestListener opens a loopback listener for a refusal behavior test.
func openTestListener(t *testing.T) net.Listener {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for refusal behavior test: %v", err)
	}
	return ln
}

// refusal dials addr, closes the connection when ctx ends, and accepts only a
// peer-initiated connection closure as refusal.
func refusal(ctx context.Context, addr string) (result error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("dial refusal check canceled: %w", ctxErr)
		}
		return fmt.Errorf("dial refused address %q: %w", addr, err)
	}
	stopClose := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer func() {
		stopClose()
		_ = conn.Close()
	}()

	var buf [1]byte
	n, readErr := conn.Read(buf[:])
	if n > 0 {
		return fmt.Errorf("endpoint sent data: received %d byte(s)", n)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("refusal check canceled: %w", ctxErr)
	}
	if readErr == nil {
		return errors.New("endpoint kept connection open without data")
	}
	if errors.Is(readErr, io.EOF) || errors.Is(readErr, syscall.ECONNRESET) {
		return nil
	}
	return fmt.Errorf("read refused address: %w", readErr)
}

// assertRequestRefused fails unless the issue 1193 listener closes the TCP
// connection without sending data.
func assertRequestRefused(tb testing.TB, url string) {
	tb.Helper()
	addr := strings.TrimPrefix(url, "http://")
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	if err := refusal(ctx, addr); err != nil {
		tb.Fatalf("refused request check: %v", err)
	}
}
