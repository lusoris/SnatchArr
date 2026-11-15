// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"code.dny.dev/ssrf"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

const (
	tunnelBufSize        = 32 * 1024
	maxPumpReads         = 1 << 30
	establishedStatusRaw = "HTTP/1.1 200 Connection established\r\n\r\n"
	sideClient           = "client"
	sideUpstream         = "upstream"
)

// DialControl is the net.Dialer.Control hook signature used to guard outbound dials.
type DialControl func(network, address string, c syscall.RawConn) error

func buildGuardianControl(compiled *config.Compiled) DialControl {
	opts := []ssrf.Option{ssrf.WithPorts(compiled.PortList...)}
	if len(compiled.DeniedV4) > 0 {
		opts = append(opts, ssrf.WithDeniedV4Prefixes(compiled.DeniedV4...))
	}
	if len(compiled.DeniedV6) > 0 {
		opts = append(opts, ssrf.WithDeniedV6Prefixes(compiled.DeniedV6...))
	}
	return ssrf.New(opts...).Safe
}

func (s *Service) dialUpstream(ctx context.Context, targetAddr string) (net.Conn, int, string, error) {
	opts := s.compiled.Options
	dialCtx, cancel := context.WithTimeout(ctx, opts.DialTimeout)
	defer cancel()

	dialer := net.Dialer{
		Timeout: opts.DialTimeout,
		Control: s.dialControl,
	}
	conn, err := dialer.DialContext(dialCtx, "tcp", targetAddr)
	if err == nil {
		return conn, http.StatusOK, metrics.ResultOK, nil
	}
	status, result := classifyDialError(err, dialCtx)
	return nil, status, result, fmt.Errorf("proxy: dial %s: %w", targetAddr, err)
}

func classifyDialError(err error, dialCtx context.Context) (int, string) {
	if isGuardRefusal(err) {
		return http.StatusForbidden, metrics.ResultRefusedIP
	}
	if isTimeoutError(err, dialCtx) {
		return http.StatusGatewayTimeout, metrics.ResultDialTimeout
	}
	return http.StatusBadGateway, metrics.ResultDialError
}

func isGuardRefusal(err error) bool {
	return errors.Is(err, ssrf.ErrProhibitedIP) ||
		errors.Is(err, ssrf.ErrProhibitedPort) ||
		errors.Is(err, ssrf.ErrProhibitedNetwork) ||
		errors.Is(err, ssrf.ErrInvalidHostPort)
}

func isTimeoutError(err error, dialCtx context.Context) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(dialCtx.Err(), context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func closeConn(logger *slog.Logger, side string, conn net.Conn) {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		logger.Debug("metadata-proxy: close tunnel conn",
			slog.String("side", side),
			slog.Any("err", err),
		)
	}
}

func (s *Service) bridgeTunnel(w http.ResponseWriter, upstream net.Conn) (int64, int64, bool) {
	clientConn, clientBuf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		closeConn(s.logger, sideUpstream, upstream)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return 0, 0, false
	}
	if err := clientConn.SetDeadline(time.Time{}); err != nil {
		s.logger.Debug("metadata-proxy: clear hijacked deadline failed", slog.Any("err", err))
		closeConn(s.logger, sideClient, clientConn)
		closeConn(s.logger, sideUpstream, upstream)
		return 0, 0, false
	}

	opts := s.compiled.Options
	tunnelDeadline := s.clock.Now().Add(opts.MaxTunnel)
	if err := writeEstablished(clientConn, s.clock, opts.IdleTimeout, tunnelDeadline); err != nil {
		closeConn(s.logger, sideClient, clientConn)
		closeConn(s.logger, sideUpstream, upstream)
		return 0, 0, false
	}
	initialUp, ok := flushBufferedClientBytes(upstream, clientBuf, s.clock, opts.IdleTimeout, tunnelDeadline)
	if !ok {
		closeConn(s.logger, sideClient, clientConn)
		closeConn(s.logger, sideUpstream, upstream)
		return initialUp, 0, true
	}
	up, down := s.copyBothWays(clientConn, upstream, initialUp, tunnelDeadline)
	return up, down, true
}

func writeEstablished(clientConn net.Conn, clk clock.Clock, idleTimeout time.Duration, tunnelDeadline time.Time) error {
	if err := clientConn.SetWriteDeadline(stepDeadline(clk.Now(), idleTimeout, tunnelDeadline)); err != nil {
		return fmt.Errorf("proxy: set 200 write deadline: %w", err)
	}
	if _, err := io.WriteString(clientConn, establishedStatusRaw); err != nil {
		return fmt.Errorf("proxy: write 200 response: %w", err)
	}
	if err := clientConn.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("proxy: clear 200 write deadline: %w", err)
	}
	return nil
}

func flushBufferedClientBytes(
	upstream net.Conn,
	clientBuf *bufio.ReadWriter,
	clk clock.Clock,
	idleTimeout time.Duration,
	tunnelDeadline time.Time,
) (int64, bool) {
	if clientBuf == nil || clientBuf.Reader.Buffered() == 0 {
		return 0, true
	}
	n := clientBuf.Reader.Buffered()
	buf := make([]byte, n)
	readN, err := io.ReadFull(clientBuf.Reader, buf)
	if readN == 0 {
		return 0, err == nil
	}
	if deadlineErr := upstream.SetWriteDeadline(stepDeadline(clk.Now(), idleTimeout, tunnelDeadline)); deadlineErr != nil {
		return 0, false
	}
	wroteN, writeErr := upstream.Write(buf[:readN])
	return int64(wroteN), err == nil && writeErr == nil
}

func (s *Service) copyBothWays(
	clientConn, upstream net.Conn,
	initialUp int64,
	tunnelDeadline time.Time,
) (int64, int64) {
	opts := s.compiled.Options
	done := make(chan struct{})
	go watchTunnel(done, s.clock, opts.MaxTunnel, s.stopCh, s.logger, clientConn, upstream)

	upCh := make(chan int64, 1)
	go func() {
		n := initialUp + pump(upstream, clientConn, opts.IdleTimeout, tunnelDeadline, s.clock)
		closeConn(s.logger, sideUpstream, upstream)
		closeConn(s.logger, sideClient, clientConn)
		upCh <- n
	}()

	down := pump(clientConn, upstream, opts.IdleTimeout, tunnelDeadline, s.clock)
	closeConn(s.logger, sideClient, clientConn)
	closeConn(s.logger, sideUpstream, upstream)
	up := <-upCh
	close(done)
	return up, down
}

func watchTunnel(
	done <-chan struct{},
	clk clock.Clock,
	maxTunnel time.Duration,
	stopCh <-chan struct{},
	logger *slog.Logger,
	clientConn, upstream net.Conn,
) {
	maxTimer := clk.NewTimer(maxTunnel)
	defer maxTimer.Stop()
	select {
	case <-done:
		return
	case <-maxTimer.Chan():
	case <-stopCh:
	}
	closeConn(logger, sideClient, clientConn)
	closeConn(logger, sideUpstream, upstream)
}

func pump(dst, src net.Conn, idleTimeout time.Duration, tunnelDeadline time.Time, clk clock.Clock) int64 {
	buf := make([]byte, tunnelBufSize)
	var total int64
	for range maxPumpReads {
		if err := src.SetReadDeadline(stepDeadline(clk.Now(), idleTimeout, tunnelDeadline)); err != nil {
			return total
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := dst.SetWriteDeadline(stepDeadline(clk.Now(), idleTimeout, tunnelDeadline)); err != nil {
				return total
			}
			wrote, writeErr := dst.Write(buf[:n])
			total += int64(wrote)
			if writeErr != nil {
				return total
			}
		}
		if readErr != nil {
			return total
		}
	}
	return total
}

func stepDeadline(now time.Time, idleTimeout time.Duration, tunnelDeadline time.Time) time.Time {
	idleDeadline := now.Add(idleTimeout)
	if tunnelDeadline.Before(idleDeadline) {
		return tunnelDeadline
	}
	return idleDeadline
}
