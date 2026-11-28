// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestMapWriteError(t *testing.T) {
	t.Parallel()

	const exclName = "library_processing_rules_library_path_excl"
	fk := &pgconn.PgError{Code: "23503"}
	namedExcl := &pgconn.PgError{Code: "23P01", ConstraintName: exclName}
	unnamedExcl := &pgconn.PgError{Code: "23P01"}
	otherExcl := &pgconn.PgError{Code: "23P01", ConstraintName: "other_excl"}
	unique := &pgconn.PgError{Code: "23505"}
	plain := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"foreign key maps to not found", fk, domain.ErrNotFound},
		{"named exclusion maps to conflict", namedExcl, domain.ErrConflict},
		{"unnamed exclusion propagates", unnamedExcl, unnamedExcl},
		{"other exclusion propagates", otherExcl, otherExcl},
		{"unique violation maps to conflict", unique, domain.ErrConflict},
		{"wrapped fk keeps not found", fmt.Errorf("insert: %w", fk), domain.ErrNotFound},
		{"wrapped named exclusion keeps conflict", fmt.Errorf("update: %w", namedExcl), domain.ErrConflict},
		{"wrapped unique keeps conflict", fmt.Errorf("insert: %w", unique), domain.ErrConflict},
		{"unrelated error propagates", plain, plain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapWriteError(tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapWriteError(%v) = %v; want errors.Is %v", tt.err, got, tt.want)
			}
		})
	}
}
