// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package adult provides Stash-box multi-instance federation, favourite sync, and search.
package adult

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// StashboxEndpoint identifies a configured stash-box instance.
type StashboxEndpoint struct {
	InstanceID uuid.UUID
	Endpoint   string
}

// EntityLink represents a foreign reference on a stash-box instance.
type EntityLink struct {
	InstanceID uuid.UUID
	ForeignID  string
}

// Candidate represents an existing favorite entity evaluated for merging.
type Candidate struct {
	ID             uuid.UUID
	Name           string
	Disambiguation string
	URLs           []string
	Links          []EntityLink
}

// IncomingEntity represents an entity fetched from a stash-box instance.
type IncomingEntity struct {
	Name           string
	Disambiguation string
	URLs           []string
}

// NormalizeName returns the trimmed, lowercase, space-collapsed form of name.
func NormalizeName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// NormalizeExternalURL normalizes an external site URL by lowercasing the host,
// stripping leading 'www.', trimming trailing slashes from the path, and omitting the scheme.
func NormalizeExternalURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("adult: parse url %q: %w", raw, err)
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	cleanedPath := strings.TrimRight(u.Path, "/")
	res := host + cleanedPath
	if u.RawQuery != "" {
		res += "?" + u.RawQuery
	}
	return res, nil
}

// matchEndpointForeignID checks if rawURL points at ep.Endpoint and extracts the foreign ID segment.
func matchEndpointForeignID(endpoint string, rawURL string) (string, bool) {
	epURL, err := url.Parse(endpoint)
	if err != nil {
		return "", false
	}
	targetURL, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	epHost := strings.ToLower(strings.TrimPrefix(epURL.Host, "www."))
	targetHost := strings.ToLower(strings.TrimPrefix(targetURL.Host, "www."))
	if epHost == "" || epHost != targetHost {
		return "", false
	}

	trimmedPath := strings.TrimRight(targetURL.Path, "/")
	parts := strings.Split(strings.Trim(trimmedPath, "/"), "/")
	if len(parts) >= 2 {
		kind := parts[len(parts)-2]
		if kind == "performers" || kind == "studios" {
			return parts[len(parts)-1], true
		}
	}
	return "", false
}

func matchByEndpointURL(incoming IncomingEntity, c Candidate, endpoints []StashboxEndpoint) bool {
	for _, rawURL := range incoming.URLs {
		for _, ep := range endpoints {
			fID, ok := matchEndpointForeignID(ep.Endpoint, rawURL)
			if !ok {
				continue
			}
			for _, link := range c.Links {
				if link.InstanceID == ep.InstanceID && link.ForeignID == fID {
					return true
				}
			}
		}
	}
	return false
}

func matchByExternalURL(incoming IncomingEntity, c Candidate) bool {
	if len(incoming.URLs) == 0 || len(c.URLs) == 0 {
		return false
	}
	candidateNorms := make(map[string]struct{}, len(c.URLs))
	for _, u := range c.URLs {
		if norm, err := NormalizeExternalURL(u); err == nil && norm != "" {
			candidateNorms[norm] = struct{}{}
		}
	}
	for _, u := range incoming.URLs {
		norm, err := NormalizeExternalURL(u)
		if err != nil || norm == "" {
			continue
		}
		if _, exists := candidateNorms[norm]; exists {
			return true
		}
	}
	return false
}

func matchByName(incoming IncomingEntity, c Candidate) bool {
	if strings.TrimSpace(incoming.Disambiguation) != "" || strings.TrimSpace(c.Disambiguation) != "" {
		return false
	}
	normInc := NormalizeName(incoming.Name)
	normCand := NormalizeName(c.Name)
	return normInc != "" && normInc == normCand
}

// MatchKind classifies how an incoming entity matched an existing candidate.
type MatchKind string

const (
	// MatchNone indicates no match or correlation was found.
	MatchNone MatchKind = ""
	// MatchAutoMerge indicates a strong identifier match (endpoint foreign ID or normalized external URL) safe for automatic merging.
	MatchAutoMerge MatchKind = "auto_merge"
	// MatchAuditCandidate indicates a weak name-only correlation that must be routed to the audit queue rather than auto-merged.
	MatchAuditCandidate MatchKind = "audit_candidate"
)

// AuditCandidate records a pair of entities that share a normalized name without a strong identifier match.
type AuditCandidate struct {
	Existing Candidate
	Incoming Candidate
}

// MatchEntity attempts to match an incoming entity against existing candidates according to:
// (1) URLs pointing at another configured stash-box endpoint whose foreign ID we already hold (MatchAutoMerge).
// (2) Normalized external site URL (lowercase host without www., trimmed trailing slash, scheme ignored) (MatchAutoMerge).
// (3) Exact normalized name when disambiguation is empty on both entities (MatchAuditCandidate; never auto-merged).
// Returns the matched candidate, its index, and the MatchKind, or (nil, -1, MatchNone) if no candidate matches.
func MatchEntity(incoming IncomingEntity, candidates []Candidate, endpoints []StashboxEndpoint) (*Candidate, int, MatchKind) {
	for i := range candidates {
		if matchByEndpointURL(incoming, candidates[i], endpoints) {
			return &candidates[i], i, MatchAutoMerge
		}
	}
	for i := range candidates {
		if matchByExternalURL(incoming, candidates[i]) {
			return &candidates[i], i, MatchAutoMerge
		}
	}
	for i := range candidates {
		if matchByName(incoming, candidates[i]) {
			return &candidates[i], i, MatchAuditCandidate
		}
	}
	return nil, -1, MatchNone
}

// DeduplicateEntitiesWithAudit collapses a list of candidates using strong auto-merge rules
// and returns any name-only correlations as audit candidates instead of merging them.
func DeduplicateEntitiesWithAudit(items []Candidate, endpoints []StashboxEndpoint) ([]Candidate, []AuditCandidate) {
	var deduped []Candidate
	var audit []AuditCandidate
	for _, item := range items {
		inc := IncomingEntity{
			Name:           item.Name,
			Disambiguation: item.Disambiguation,
			URLs:           item.URLs,
		}
		matched, _, kind := MatchEntity(inc, deduped, endpoints)
		if matched != nil && kind == MatchAutoMerge {
			matched.URLs = append(matched.URLs, item.URLs...)
			matched.Links = append(matched.Links, item.Links...)
			continue
		}
		if matched != nil && kind == MatchAuditCandidate {
			audit = append(audit, AuditCandidate{
				Existing: *matched,
				Incoming: item,
			})
		}
		deduped = append(deduped, item)
	}
	return deduped, audit
}

// DeduplicateEntities collapses a list of candidates into a deduplicated slice using strong auto-merge rules.
func DeduplicateEntities(items []Candidate, endpoints []StashboxEndpoint) []Candidate {
	deduped, _ := DeduplicateEntitiesWithAudit(items, endpoints)
	return deduped
}
