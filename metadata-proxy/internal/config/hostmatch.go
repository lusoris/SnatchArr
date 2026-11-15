// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

const (
	maxDNSNameLen  = 253
	maxDNSLabelLen = 63
)

// ErrBadHostPattern reports an invalid host entry in allow_hosts or refuse_hosts.
var ErrBadHostPattern = errors.New("config: invalid host pattern")

// NormalizeHost lowercases raw, trims surrounding whitespace, and strips one trailing dot.
func NormalizeHost(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	return strings.TrimSuffix(s, ".")
}

// ValidateHostPattern normalizes and validates an exact host or leading-dot suffix pattern.
func ValidateHostPattern(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w %q: empty or contains surrounding whitespace", ErrBadHostPattern, raw)
	}
	trimmed := strings.TrimSuffix(raw, ".")
	if trimmed == "" || strings.HasSuffix(trimmed, ".") {
		return "", fmt.Errorf("%w %q: empty host or consecutive trailing dots", ErrBadHostPattern, raw)
	}
	lower := strings.ToLower(trimmed)
	domain := strings.TrimPrefix(lower, ".")
	if err := validateDNSName(domain, raw); err != nil {
		return "", err
	}
	return lower, nil
}

func validateDNSName(domain, raw string) error {
	if domain == "" || len(domain) > maxDNSNameLen {
		return fmt.Errorf("%w %q: host length must be 1..%d", ErrBadHostPattern, raw, maxDNSNameLen)
	}
	if _, err := netip.ParseAddr(domain); err == nil {
		return fmt.Errorf("%w %q: IP literals belong in denied_prefixes, not host lists", ErrBadHostPattern, raw)
	}
	for label := range strings.SplitSeq(domain, ".") {
		if !isValidDNSLabel(label) {
			return fmt.Errorf("%w %q: invalid DNS label %q", ErrBadHostPattern, raw, label)
		}
	}
	return nil
}

func isValidDNSLabel(label string) bool {
	if label == "" || len(label) > maxDNSLabelLen {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := range len(label) {
		if !isDNSLabelByte(label[i]) {
			return false
		}
	}
	return true
}

func isDNSLabelByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
}

// HostMatcher matches request hosts against exact hosts and leading-dot domain suffixes.
type HostMatcher struct {
	exact    map[string]struct{}
	suffixes []string
	patterns []string
}

// NewHostMatcher validates patterns and builds an immutable NO_PROXY-style host matcher.
func NewHostMatcher(patterns []string) (HostMatcher, error) {
	m := HostMatcher{
		exact:    make(map[string]struct{}, len(patterns)),
		suffixes: make([]string, 0, len(patterns)),
		patterns: make([]string, 0, len(patterns)),
	}
	seenSuffix := make(map[string]struct{}, len(patterns))
	for _, raw := range patterns {
		norm, err := ValidateHostPattern(raw)
		if err != nil {
			return HostMatcher{}, err
		}
		if strings.HasPrefix(norm, ".") {
			if _, dup := seenSuffix[norm]; !dup {
				seenSuffix[norm] = struct{}{}
				m.suffixes = append(m.suffixes, norm)
				m.patterns = append(m.patterns, norm)
			}
			continue
		}
		if _, dup := m.exact[norm]; !dup {
			m.exact[norm] = struct{}{}
			m.patterns = append(m.patterns, norm)
		}
	}
	return m, nil
}

// Match returns the configured pattern that matches host (exact first, then suffix).
func (m HostMatcher) Match(host string) (string, bool) {
	norm := NormalizeHost(host)
	if norm == "" || strings.HasSuffix(norm, ".") {
		return "", false
	}
	if _, ok := m.exact[norm]; ok {
		return norm, true
	}
	for _, suf := range m.suffixes {
		if len(norm) > len(suf) && strings.HasSuffix(norm, suf) {
			return suf, true
		}
	}
	return "", false
}

// Matches reports whether host matches any configured exact or suffix pattern.
func (m HostMatcher) Matches(host string) bool {
	_, ok := m.Match(host)
	return ok
}

// Patterns returns the normalized, deduplicated patterns in configuration order.
func (m HostMatcher) Patterns() []string {
	return slices.Clone(m.patterns)
}
