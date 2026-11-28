// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

func TestListLibraryProcessingRulesPageRejectsExplicitZeroLimit(t *testing.T) {
	t.Parallel()
	// A non-nil service passes the module check; validation must not query it.
	h := &Handlers{libraryProcessing: new(libraryprocessing.Service)}
	params := oas.ListLibraryProcessingRulesPageParams{}
	params.Limit.SetTo(0)

	page, err := h.ListLibraryProcessingRulesPage(context.Background(), params)
	if !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("explicit zero limit: got error %v, want %v", err, domain.ErrBadRequest)
	}
	if page != nil {
		t.Fatalf("explicit zero limit: got page %+v, want nil", page)
	}
}
