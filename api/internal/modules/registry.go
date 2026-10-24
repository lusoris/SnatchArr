// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules

import (
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const maxModules = 32

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Registry is an immutable catalog of declared optional module descriptors.
type Registry struct {
	byID        map[string]Descriptor
	ids         []string
	descriptors []Descriptor
}

// NewRegistry builds and validates an immutable Registry from a static slice of descriptors.
func NewRegistry(descriptors []Descriptor) (*Registry, error) {
	if len(descriptors) > maxModules {
		return nil, fmt.Errorf("%w: module count %d exceeds maximum of %d", domain.ErrInvalid, len(descriptors), maxModules)
	}
	byID := make(map[string]Descriptor, len(descriptors))
	ids := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		if !idPattern.MatchString(d.ID) {
			return nil, fmt.Errorf("%w: invalid module id %q", domain.ErrInvalid, d.ID)
		}
		if _, exists := byID[d.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate module id %q", domain.ErrConflict, d.ID)
		}
		if d.ValidateConfig == nil {
			return nil, fmt.Errorf("%w: module %q has nil ValidateConfig", domain.ErrInvalid, d.ID)
		}
		byID[d.ID] = d
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	descs := make([]Descriptor, len(ids))
	for i, id := range ids {
		descs[i] = byID[id]
	}
	return &Registry{
		byID:        byID,
		ids:         ids,
		descriptors: descs,
	}, nil
}

// DefaultRegistry creates an immutable Registry containing all built-in modules.
func DefaultRegistry() (*Registry, error) {
	return NewRegistry(DefaultDescriptors())
}

// Descriptors returns a copy of registered module descriptors sorted by ID.
func (r *Registry) Descriptors() []Descriptor {
	return slices.Clone(r.descriptors)
}

// Lookup finds a module descriptor by ID.
func (r *Registry) Lookup(id string) (Descriptor, bool) {
	d, ok := r.byID[id]
	return d, ok
}

// IDs returns a sorted copy of registered module IDs.
func (r *Registry) IDs() []string {
	return slices.Clone(r.ids)
}
