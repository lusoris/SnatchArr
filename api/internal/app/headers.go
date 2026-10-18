// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import "github.com/golusoris/golusoris/httpx/middleware"

// headerPolicy is the Content-Security-Policy header of every response. The SPA's own
// policy (script, style and connect sources, by hash) travels in the meta tag of its
// static page (web/vite.config.ts) and both policies apply, so this one names only the
// directives a meta tag cannot carry or that hold for every response, API included.
const headerPolicy = "frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'"

// permissionsPolicy denies the browser features the UI never uses.
const permissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=()"

// hstsPolicy pins HTTPS for a year once a browser has seen it over TLS; browsers ignore it
// on plain HTTP.
const hstsPolicy = "max-age=31536000; includeSubDomains"

// securityHeaders extends the golusoris baseline (frame, nosniff, referrer) with CSP and
// Permissions-Policy, and with HSTS when cookies are Secure, which means the deployment is
// served over HTTPS (snatcharr.session.secure).
func securityHeaders(secureCookies bool) middleware.SecureHeadersOptions {
	opts := middleware.SecureHeadersDefaults()
	opts.ContentSecurityPolicy = headerPolicy
	opts.PermissionsPolicy = permissionsPolicy
	if secureCookies {
		opts.HSTS = hstsPolicy
	}
	return opts
}
