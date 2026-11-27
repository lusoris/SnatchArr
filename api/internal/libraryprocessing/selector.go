// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package libraryprocessing stores scoped library processing rules and selects
// the effective rule for one library item (ADR-0041 section 10, ADR-0050 section 2).
package libraryprocessing

import (
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Scope names one level of the rule precedence hierarchy. A lower numeric
// value has higher priority. The zero Scope is not valid and means "no match".
type Scope uint8

// Scopes in descending priority: ScopeItemOverride beats everything,
// ScopeGlobalDefault only applies when nothing else matched.
const (
	ScopeItemOverride  Scope = iota + 1 // per-item override.
	ScopeUserLanguage                   // per-user request language preference.
	ScopeLibrary                        // per-library / root folder.
	ScopeInstance                       // per-*arr instance.
	ScopeMediaType                      // per-media-type.
	ScopeGlobalDefault                  // global default.
)

// Precedence returns all scopes from highest to lowest priority.
func Precedence() [6]Scope {
	return [6]Scope{
		ScopeItemOverride, ScopeUserLanguage, ScopeLibrary,
		ScopeInstance, ScopeMediaType, ScopeGlobalDefault,
	}
}

// Valid reports whether s is one of the six defined scopes.
func (s Scope) Valid() bool { return s >= ScopeItemOverride && s <= ScopeGlobalDefault }

// String returns a stable label for RuleTrace output, or "unknown" if s is
// not a defined scope.
func (s Scope) String() string {
	switch s {
	case ScopeItemOverride:
		return "item_override"
	case ScopeUserLanguage:
		return "user_language"
	case ScopeLibrary:
		return "library"
	case ScopeInstance:
		return "instance"
	case ScopeMediaType:
		return "media_type"
	case ScopeGlobalDefault:
		return "global_default"
	default:
		return "unknown"
	}
}

// ParseScope parses a scope identifier string into a validated Scope.
func ParseScope(raw string) (Scope, error) {
	switch raw {
	case "item_override":
		return ScopeItemOverride, nil
	case "user_language":
		return ScopeUserLanguage, nil
	case "library":
		return ScopeLibrary, nil
	case "instance":
		return ScopeInstance, nil
	case "media_type":
		return ScopeMediaType, nil
	case "global_default":
		return ScopeGlobalDefault, nil
	default:
		return 0, fmt.Errorf("%w: invalid scope %q", domain.ErrInvalid, raw)
	}
}

// Rules holds the optional matching rule for each scope of one item. A nil
// field means that scope has no matching rule; a non-nil pointer is a match,
// even if it points at the zero value of R. Named fields keep priority
// independent of any storage or iteration order. R is treated as an opaque,
// immutable value and is never modified or copied.
type Rules[R any] struct {
	ItemOverride  *R
	UserLanguage  *R
	Library       *R
	Instance      *R
	MediaType     *R
	GlobalDefault *R
}

// Selection is the winning rule and the scope it came from.
type Selection[R any] struct {
	Scope Scope
	Rule  *R // the caller's pointer, unchanged.
}

// Select returns the highest-priority present rule. The boolean is false when
// no scope has a rule, in which case the Selection is the zero value.
func Select[R any](r Rules[R]) (Selection[R], bool) {
	cands := [...]Selection[R]{
		{ScopeItemOverride, r.ItemOverride},
		{ScopeUserLanguage, r.UserLanguage},
		{ScopeLibrary, r.Library},
		{ScopeInstance, r.Instance},
		{ScopeMediaType, r.MediaType},
		{ScopeGlobalDefault, r.GlobalDefault},
	}
	for _, c := range cands {
		if c.Rule != nil {
			return c, true
		}
	}
	return Selection[R]{}, false
}
