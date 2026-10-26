// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const (
	// JellyfinRevalidateAfter is the cache TTL after which a Jellyfin member is re-read.
	JellyfinRevalidateAfter = 15 * time.Minute

	// JellyfinCacheCapacity is the maximum number of subjects tracked in the revalidation cache.
	JellyfinCacheCapacity = 4096

	// ReasonMemberMissing indicates the Jellyfin user was not found.
	ReasonMemberMissing = "jellyfin_member_missing"
	// ReasonServiceUnauthorized indicates the Jellyfin service API key was refused.
	ReasonServiceUnauthorized = "jellyfin_service_unauthorized"
	// ReasonMemberDisabled indicates the Jellyfin user account is disabled.
	ReasonMemberDisabled = "jellyfin_member_disabled"
	// ReasonRoleChanged indicates the role derived now differs from the session role.
	ReasonRoleChanged = "jellyfin_role_changed"
)

// Revalidator manages per-request and periodic member policy revalidation against Jellyfin.
type Revalidator struct {
	auth        Authenticator
	clk         clock.Clock
	defaultRole domain.Role
	timeout     time.Duration
	logger      *slog.Logger

	mu    sync.Mutex
	cache map[string]time.Time
}

func cacheKey(subject string, role domain.Role) string {
	return subject + "\x00" + string(role)
}

// NewRevalidator creates a new Jellyfin session revalidator.
func NewRevalidator(auth Authenticator, clk clock.Clock, defaultRole domain.Role, timeout time.Duration, logger *slog.Logger) *Revalidator {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Revalidator{
		auth:        auth,
		clk:         clk,
		defaultRole: defaultRole,
		timeout:     timeout,
		logger:      logger,
		cache:       make(map[string]time.Time),
	}
}

// Revalidate checks if subject's session is still valid for currentRole.
// If invalid, it returns the revocation reason; on fresh cache, transient error,
// or valid session, it returns "".
func (r *Revalidator) Revalidate(ctx context.Context, subject string, currentRole domain.Role) string {
	key := cacheKey(subject, currentRole)
	now := r.clk.Now()
	r.mu.Lock()
	lastCheck, ok := r.cache[key]
	r.mu.Unlock()

	if ok && now.Sub(lastCheck) < JellyfinRevalidateAfter {
		return ""
	}

	m, reason, err := r.RevalidateSubject(ctx, subject)
	if err != nil {
		if r.logger != nil {
			r.logger.WarnContext(ctx, "jellyfin: session revalidation failed",
				slog.String("subject", subject),
				slog.String("error", err.Error()),
			)
		}
		return ""
	}
	if reason != "" {
		return reason
	}
	if roleReason := r.CheckRole(m, currentRole); roleReason != "" {
		return roleReason
	}
	r.touch(key, now)
	return ""
}

// RevalidateSubject fetches subject from Jellyfin and returns the member and revocation
// reason without updating the cache. Transient errors return non-nil error.
func (r *Revalidator) RevalidateSubject(ctx context.Context, subject string) (Member, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	m, err := r.auth.User(callCtx, subject)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return Member{}, ReasonMemberMissing, nil
		}
		if errors.Is(err, domain.ErrUnauthorized) {
			return Member{}, ReasonServiceUnauthorized, nil
		}
		return Member{}, "", err
	}
	if m.Disabled {
		return m, ReasonMemberDisabled, nil
	}
	return m, "", nil
}

// CheckRole reports ReasonRoleChanged if the derived role does not match currentRole.
func (r *Revalidator) CheckRole(m Member, currentRole domain.Role) string {
	derivedRole := domain.RoleForJellyfinMember(m.Admin, r.defaultRole)
	if derivedRole != currentRole {
		return ReasonRoleChanged
	}
	return ""
}

func (r *Revalidator) touch(key string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cache[key]; !exists && len(r.cache) >= JellyfinCacheCapacity {
		r.evictOldestLocked()
	}
	r.cache[key] = now
}

func (r *Revalidator) evictOldestLocked() {
	// When cache reaches capacity (4096 entries), the oldest entry by check time
	// is evicted to prevent thundering herd re-checks that would occur with a full reset.
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, t := range r.cache {
		if first || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
			first = false
		}
	}
	if oldestKey != "" {
		delete(r.cache, oldestKey)
	}
}
