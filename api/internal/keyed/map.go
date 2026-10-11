// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package keyed holds the small in-memory map a service keeps per database row, such as the
// last observed status of a link. It adds no expiry: the caller stores the time of the
// observation in the value and decides how long it trusts it.
package keyed

import "sync"

// Map is a map safe for concurrent use. The zero value is empty and ready.
type Map[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]V
}

// Get returns the value stored for key and whether there is one.
func (c *Map[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

// Put stores value for key and replaces an earlier one.
func (c *Map[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[K]V)
	}
	c.m[key] = value
}

// Forget drops key; a key that is not stored is a no-op.
func (c *Map[K, V]) Forget(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}
