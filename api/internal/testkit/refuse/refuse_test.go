// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package refuse

import (
	"context"
	"errors"
	"net"
	"net/http"
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
	client := &http.Client{Timeout: 5 * time.Second}
	assertRequestRefused(t, client, url)
}

// TestListener_PortRemainsBound checks that issue 1193 cannot recur through a
// parallel listener rebinding the refused address.
func TestListener_PortRemainsBound(t *testing.T) {
	t.Parallel()
	url := Listener(t)
	addr := strings.TrimPrefix(url, "http://")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err == nil {
		if closeErr := ln.Close(); closeErr != nil {
			t.Errorf("close unexpected listener: %v", closeErr)
		}
		t.Fatal("another listener bound the refused port")
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("rebind failed for another reason: %v", err)
	}
}

// TestListener_BoundaryMinusOneRequestsFail checks the bounded accept loop
// continues refusing through its supported issue 1193 request boundary.
func TestListener_BoundaryMinusOneRequestsFail(t *testing.T) {
	t.Parallel()
	url := Listener(t)
	client := &http.Client{Timeout: 5 * time.Second}
	for range maxConnections - 1 {
		assertRequestRefused(t, client, url)
	}
}

// TestListener_LifetimeEndsAcceptLoop checks the issue 1193 listener closes
// its accept goroutine when the constructor's lifetime bound expires.
func TestListener_LifetimeEndsAcceptLoop(t *testing.T) {
	t.Parallel()
	_, done := listener(t, 10*time.Millisecond)
	await.Await(t, done, "lifetime-bounded refusing listener")
}

// assertRequestRefused fails when the issue 1193 listener serves a response
// instead of closing the accepted connection.
func assertRequestRefused(tb testing.TB, client *http.Client, url string) {
	tb.Helper()
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		tb.Errorf("close unexpected response: %v", closeErr)
	}
	tb.Fatal("request unexpectedly succeeded")
}
