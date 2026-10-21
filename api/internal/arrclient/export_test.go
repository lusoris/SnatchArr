// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import "net/http"

// HTTPClient exports the unexported httpClient helper for external tests.
func (f BaseFactory) HTTPClient() *http.Client {
	return f.httpClient()
}

// ClientBaseTransport extracts the base RoundTripper wrapped inside apiTransport.
func ClientBaseTransport(c *http.Client) http.RoundTripper {
	if t, ok := c.Transport.(apiTransport); ok {
		return t.base
	}
	return nil
}

// CloneTransport exports cloneTransport for testing transport fallback behavior.
func CloneTransport(rt http.RoundTripper) http.RoundTripper {
	return cloneTransport(rt)
}
