// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin

import (
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// CacheLen returns the number of entries currently in the cache (primarily for tests).
func (r *Revalidator) CacheLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cache)
}

// SetCache sets the check timestamp for subject and role (primarily for tests).
func (r *Revalidator) SetCache(subject string, role domain.Role, t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[cacheKey(subject, role)] = t
}
