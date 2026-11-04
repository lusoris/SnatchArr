// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policies

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestEnsureErr(t *testing.T) {
	t.Parallel()
	fk := &pgconn.PgError{Code: "23503"}
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"positive no error", nil, nil},
		{"negative deleted instance", fk, domain.ErrNotFound},
		{"boundary wrapped foreign key violation", fmt.Errorf("insert: %w", fk), domain.ErrNotFound},
		{"negative unique violation stays conflict", &pgconn.PgError{Code: "23505"}, domain.ErrConflict},
		{"negative other database error preserved", &pgconn.PgError{Code: "08006"}, nil},
		{"negative canceled request preserved", context.Canceled, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := tc.want
			if want == nil {
				want = tc.err
			}
			if got := ensureErr(tc.err); !errors.Is(got, want) {
				t.Errorf("ensureErr(%v) = %v, want %v", tc.err, got, want)
			}
		})
	}
}
