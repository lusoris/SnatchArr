// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

const maxRefusedConnections = 256

// refusingListener reserves a loopback port and closes accepted connections at once.
// A freed port can be rebound by a parallel test server (issue 1193).
func refusingListener(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for refused requests: %v", err)
	}
	tcpLn := ln.(*net.TCPListener)
	if err := tcpLn.SetDeadline(time.Now().Add(20 * time.Minute)); err != nil {
		if closeErr := tcpLn.Close(); closeErr != nil {
			t.Errorf("close listener after deadline error: %v", closeErr)
		}
		t.Fatalf("set listener deadline: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		for range maxRefusedConnections {
			conn, acceptErr := tcpLn.Accept()
			if errors.Is(acceptErr, net.ErrClosed) {
				done <- nil
				return
			}
			if acceptErr != nil {
				done <- fmt.Errorf("accept refused request: %w", acceptErr)
				return
			}
			if deadlineErr := conn.SetDeadline(time.Now().Add(5 * time.Second)); deadlineErr != nil {
				deadlineErr = fmt.Errorf("set refused request deadline: %w", deadlineErr)
				if closeErr := conn.Close(); closeErr != nil {
					deadlineErr = errors.Join(deadlineErr, fmt.Errorf("close after deadline error: %w", closeErr))
				}
				done <- deadlineErr
				return
			}
			if closeErr := conn.Close(); closeErr != nil {
				done <- fmt.Errorf("close refused request: %w", closeErr)
				return
			}
		}
		done <- fmt.Errorf("refused more than %d connections", maxRefusedConnections)
	}()
	t.Cleanup(func() {
		if closeErr := tcpLn.Close(); closeErr != nil {
			t.Errorf("close refusing listener: %v", closeErr)
		}
		if acceptErr := <-done; acceptErr != nil {
			t.Errorf("refusing listener: %v", acceptErr)
		}
	})
	return "http://" + tcpLn.Addr().String()
}

// TestRefusingListener covers a refused request, exclusive port ownership, and
// repeated requests at the listener's boundary.
func TestRefusingListener(t *testing.T) {
	t.Parallel()
	t.Run("request is refused", func(t *testing.T) {
		t.Parallel()
		url := refusingListener(t)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Errorf("close response: %v", closeErr)
			}
			t.Fatal("request unexpectedly succeeded")
		}
	})
	t.Run("port stays reserved", func(t *testing.T) {
		t.Parallel()
		url := refusingListener(t)
		addr := url[len("http://"):]
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
	})
	t.Run("repeated requests stay refused", func(t *testing.T) {
		t.Parallel()
		url := refusingListener(t)
		client := &http.Client{Timeout: 5 * time.Second}
		for range maxRefusedConnections - 1 {
			resp, err := client.Get(url)
			if err == nil {
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("close response: %v", closeErr)
				}
				t.Fatal("request unexpectedly succeeded")
			}
		}
	})
}
