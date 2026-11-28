// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const insertGlobalDefaultRule = `
INSERT INTO library_processing_rules (id, scope, rule, created_at, updated_at)
VALUES ($1::uuid, 'global_default', $2::jsonb, $3, $3)`

// TestLibraryProcessingRulesRuleCheckConstraint runs every case in parallel on
// a shared pool. The single accepted global_default row cannot make a malformed
// row pass: PostgreSQL evaluates CHECK constraints before unique indexes, and a
// unique violation (23505) would differ from the asserted check violation
// (23514) anyway.
func TestLibraryProcessingRulesRuleCheckConstraint(t *testing.T) {
	t.Parallel()
	db := storetest.New(t).Pool()

	// One explicit deadline shared by every statement in this test. Cleanup
	// runs only after all parallel subtests finish, so the context stays alive
	// throughout them.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	rejected := []struct {
		name string
		rule string
	}{
		{"empty object", `{}`},
		{"missing track", `{"other":{"languages":["eng"]}}`},
		{"languages at top level only", `{"languages":["eng"]}`},
		{"track empty object (missing languages)", `{"track":{}}`},
		{"track json null", `{"track":null}`},
		{"track array", `{"track":[]}`},
		{"track scalar", `{"track":"x"}`},
		{"languages json null", `{"track":{"languages":null}}`},
		{"languages empty array", `{"track":{"languages":[]}}`},
		{"languages string", `{"track":{"languages":"eng"}}`},
		{"languages object", `{"track":{"languages":{}}}`},
		{"languages number", `{"track":{"languages":1}}`},
		{"rule array", `[]`},
		{"rule json null", `null`},
		{"rule scalar", `"x"`},
	}

	for _, tc := range rejected {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := db.Exec(ctx, insertGlobalDefaultRule, uuid.NewString(), tc.rule, time.Now().UTC())
			if err == nil {
				t.Fatalf("rule %s was accepted, want check violation", tc.rule)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("error is not a PostgreSQL error (non-array input must not raise jsonb_array_length errors): %v", err)
			}
			if pgErr.Code != "23514" {
				t.Fatalf("SQLSTATE = %s (%s), want 23514", pgErr.Code, pgErr.Message)
			}
			if pgErr.TableName != "library_processing_rules" {
				t.Fatalf("table = %q, want library_processing_rules", pgErr.TableName)
			}
			if pgErr.ConstraintName == "" || pgErr.ConstraintName == "library_processing_rules_scope_target_check" {
				t.Fatalf("violated constraint = %q, want the rule CHECK", pgErr.ConstraintName)
			}

			// Identify the CHECK by its definition rather than a generated name.
			var def string
			err = db.QueryRow(ctx, `
SELECT pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conrelid = 'library_processing_rules'::regclass AND contype = 'c' AND conname = $1`,
				pgErr.ConstraintName).Scan(&def)
			if err != nil {
				t.Fatalf("lookup constraint %q: %v", pgErr.ConstraintName, err)
			}
			if !strings.Contains(def, "languages") || !strings.Contains(def, "track") {
				t.Fatalf("violated constraint %q is not the rule CHECK: %s", pgErr.ConstraintName, def)
			}
		})
	}

	t.Run("accept/track languages", func(t *testing.T) {
		t.Parallel()

		id := uuid.NewString()
		if _, err := db.Exec(ctx, insertGlobalDefaultRule, id, `{"track":{"languages":["eng"]}}`, time.Now().UTC()); err != nil {
			t.Fatalf("valid rule rejected: %v", err)
		}
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM library_processing_rules WHERE id = $1::uuid`, id).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("rows = %d, want 1", n)
		}
	})
}
