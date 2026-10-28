// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestCatalogCursor_Positive_RoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	name := "Performer With Spaces & Symbols"

	encoded := EncodeCursor(name, id)
	if encoded == "" {
		t.Fatalf("expected non-empty cursor")
	}

	decName, decID, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decName != name {
		t.Errorf("name = %q, want %q", decName, name)
	}
	if decID != id {
		t.Errorf("id = %v, want %v", decID, id)
	}
}

func TestCatalogCursor_Boundary_EmptyCursor(t *testing.T) {
	t.Parallel()
	name, id, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("unexpected error on empty cursor: %v", err)
	}
	if name != "" || id != uuid.Nil {
		t.Errorf("expected empty name and nil UUID, got name=%q id=%v", name, id)
	}
}

func TestCatalogCursor_Negative_InvalidEncoding(t *testing.T) {
	t.Parallel()
	_, _, err := DecodeCursor("!!!not-base64-or-json!!!")
	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid on malformed cursor, got: %v", err)
	}
}

func TestCatalogCursor_Negative_NilUUID(t *testing.T) {
	t.Parallel()
	// Valid json with nil uuid
	encoded := EncodeCursor("name", uuid.Nil)
	_, _, err := DecodeCursor(encoded)
	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid on nil uuid in cursor, got: %v", err)
	}
}

func TestResolveCatalogLimit(t *testing.T) {
	t.Parallel()
	// Positive default
	lim, err := resolveCatalogLimit(0)
	if err != nil || lim != DefaultCatalogLimit {
		t.Errorf("limit 0 -> %d, err %v", lim, err)
	}

	// Positive valid
	lim, err = resolveCatalogLimit(100)
	if err != nil || lim != 100 {
		t.Errorf("limit 100 -> %d, err %v", lim, err)
	}

	// Boundary 1 and 200
	lim, err = resolveCatalogLimit(1)
	if err != nil || lim != 1 {
		t.Errorf("limit 1 -> %d, err %v", lim, err)
	}
	lim, err = resolveCatalogLimit(200)
	if err != nil || lim != 200 {
		t.Errorf("limit 200 -> %d, err %v", lim, err)
	}

	// Negative < 0 or > 200
	_, err = resolveCatalogLimit(-1)
	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for limit -1, got: %v", err)
	}
	_, err = resolveCatalogLimit(201)
	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for limit 201, got: %v", err)
	}
}
