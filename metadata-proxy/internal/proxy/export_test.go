// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"context"
	"net"
	"syscall"
)

// AllowAllDialControl exposes allowAllDialControl to external proxy_test tests.
var AllowAllDialControl func(string, string, syscall.RawConn) error = allowAllDialControl

// ListenFunc is the listener constructor signature injected by WithListenForTest.
type ListenFunc func(ctx context.Context, network, addr string) (net.Listener, error)

// DialUpstreamFunc is the upstream dial signature injected by WithDialForTest.
type DialUpstreamFunc func(ctx context.Context, targetAddr string) (net.Conn, int, string, error)

// WithListenForTest overrides the listener bind function on a Service in tests.
func WithListenForTest(fn ListenFunc) Option {
	return func(s *Service) {
		s.listen = fn
	}
}

// WithDialForTest overrides the upstream dial function on a Service in tests.
func WithDialForTest(fn DialUpstreamFunc) Option {
	return func(s *Service) {
		s.dial = fn
	}
}

// DialUpstreamForTest invokes s.dialUpstream directly for tests wrapping WithDialForTest.
func DialUpstreamForTest(s *Service, ctx context.Context, targetAddr string) (net.Conn, int, string, error) {
	return s.dialUpstream(ctx, targetAddr)
}
