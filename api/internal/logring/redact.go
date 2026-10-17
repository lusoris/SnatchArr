// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"regexp"
	"strings"
)

// Redacted replaces every secret the ring refuses to store.
const Redacted = "[REDACTED]"

// Size caps applied before redaction (bytes).
const (
	maxMessageBytes = 8 << 10
	maxValueBytes   = 2 << 10
)

// secretKeys are attribute names (last dotted segment, lower case) whose value is
// always replaced.
var secretKeys = map[string]bool{
	"key": true, "apikey": true, "api_key": true, "x-api-key": true,
	"password": true, "passwd": true, "token": true, "secret": true,
	"authorization": true, "cookie": true, "set-cookie": true, "webhook_url": true,
}

// secretSuffixes catch prefixed names such as sonarr_api_key or client_secret.
var secretSuffixes = []string{"_password", "_token", "_secret", "_api_key", "_apikey", "_webhook_url"}

// unredactedKeys carry 32-hex identifiers that are not secrets.
var unredactedKeys = map[string]bool{"trace_id": true, "span_id": true}

// textPatterns find secrets inside free text, in order.
var textPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Query or form parameters: ?apikey=..., &api_key=..., token=..., password=...
	{regexp.MustCompile(`(?i)\b((?:api_?key|access_token|token|password|secret)=)[^&\s"']+`), "${1}" + Redacted},
	// Header renderings: X-Api-Key: ..., Authorization: Bearer ...
	{regexp.MustCompile(`(?i)\b(x-api-key["']?\s*[:=]\s*["']?)[^\s"',}]+`), "${1}" + Redacted},
	{regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`), "${1}" + Redacted},
	// URL userinfo: scheme://user:pass@host
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@]+@`), "${1}" + Redacted + "@"},
	// *arr API keys and other 32+ character hex secrets.
	{regexp.MustCompile(`\b[0-9A-Fa-f]{32,}\b`), Redacted},
}

// isSecretKey reports whether an attribute named key (dotted path) holds a secret.
func isSecretKey(key string) bool {
	last := strings.ToLower(key[strings.LastIndexByte(key, '.')+1:])
	if secretKeys[last] {
		return true
	}
	for _, s := range secretSuffixes {
		if strings.HasSuffix(last, s) {
			return true
		}
	}
	return false
}

// redactText caps s at limit bytes and replaces every secret pattern in it.
func redactText(s string, limit int) string {
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "") + "…"
	}
	for _, p := range textPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// redactAttr returns the value stored for the attribute key.
func redactAttr(key, value string) string {
	last := strings.ToLower(key[strings.LastIndexByte(key, '.')+1:])
	switch {
	case value == "":
		return value
	case isSecretKey(key):
		return Redacted
	case unredactedKeys[last]:
		return value
	default:
		return redactText(value, maxValueBytes)
	}
}
