// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import "errors"

// Sentinel errors every layer maps onto HTTP problem details or gRPC codes.
var (
	// ErrInvalid marks a validation failure (HTTP 422).
	ErrInvalid = errors.New("invalid")
	// ErrNotFound marks a missing entity (HTTP 404).
	ErrNotFound = errors.New("not found")
	// ErrConflict marks a uniqueness or state conflict (HTTP 409).
	ErrConflict = errors.New("conflict")
	// ErrUnauthorized marks a missing or bad credential (HTTP 401).
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden marks an authenticated but disallowed action (HTTP 403).
	ErrForbidden = errors.New("forbidden")
	// ErrUnreachable marks an *arr instance that could not be contacted (HTTP 502).
	ErrUnreachable = errors.New("instance unreachable")
)
