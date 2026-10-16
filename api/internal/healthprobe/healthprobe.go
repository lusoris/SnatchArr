// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package healthprobe is the container health check behind `snatcharr-api healthcheck`.
//
// The API image is distroless: no shell, no curl. A compose or Docker health check therefore
// runs the binary itself, which asks the running API's /readyz on the loopback address and
// exits 0 only on 200. /readyz fails while Postgres is unreachable, so the container turns
// unhealthy when the API cannot serve.
package healthprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Command is the first argument that selects the probe instead of the server.
const Command = "healthcheck"

// Timeout bounds one probe (HISS-02); compose runs the next one after its own interval.
const Timeout = 3 * time.Second

// defaultListen is the default of APP_HTTP_ADDR (docs/configuration.md).
const defaultListen = ":8080"

// maxBody bounds how much of the answer is read before the connection is dropped.
const maxBody = 4096

// ErrNotReady is returned when the API answers with a status other than 200.
var ErrNotReady = errors.New("healthprobe: api not ready")

// URL returns the readiness URL for an APP_HTTP_ADDR listen address. A wildcard or empty host
// is replaced by the loopback address of the same family.
func URL(listen string) (string, error) {
	if listen == "" {
		listen = defaultListen
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("healthprobe: listen address %q: %w", listen, err)
	}
	if port == "" {
		return "", fmt.Errorf("healthprobe: listen address %q has no port", listen)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

// Check asks url once and fails unless the answer is 200 within Timeout.
func Check(ctx context.Context, client *http.Client, url string) (err error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("healthprobe: request %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthprobe: ask %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("healthprobe: close %s: %w", url, cerr)
		}
	}()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody)); err != nil {
		return fmt.Errorf("healthprobe: read %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrNotReady, url, resp.StatusCode)
	}
	return nil
}

// Run is the subcommand: it reads APP_HTTP_ADDR through getenv, probes, reports a failure on
// stderr and returns the process exit code.
func Run(ctx context.Context, getenv func(string) string, stderr io.Writer) int {
	url, err := URL(getenv("APP_HTTP_ADDR"))
	if err == nil {
		err = Check(ctx, &http.Client{Timeout: Timeout}, url)
	}
	if err != nil {
		if _, werr := fmt.Fprintln(stderr, err); werr != nil {
			return 1 // unhealthy either way; Docker reserves exit code 2
		}
		return 1
	}
	return 0
}
