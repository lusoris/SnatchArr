// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const insertLibraryRootRule = `
INSERT INTO library_processing_rules (id, scope, instance_id, root_path, rule, created_at, updated_at)
VALUES ($1::uuid, 'library', $2::uuid, $3, '{"track":{"languages":["eng"]}}'::jsonb, $4, $4)`

const libraryPathExclusion = "library_processing_rules_library_path_excl"

// libraryRoot4096 builds a deterministic absolute POSIX path of exactly 4096
// bytes. Segments are base64url-encoded SHA-256 digests of 12..43 bytes, so the
// value is high-entropy (effectively incompressible), every component stays
// well under 255 bytes, and the path has no trailing slash. A compressible
// value would be shrunk by TOAST and never hit the old btree tuple limit.
func libraryRoot4096(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for i := 0; b.Len() < 4096; i++ {
		sum := sha256.Sum256([]byte("snatcharr-library-root-" + strconv.Itoa(i)))
		seg := base64.RawURLEncoding.EncodeToString(sum[:])
		b.WriteByte('/')
		b.WriteString(seg[:12+int(sum[0])%32])
	}
	root := b.String()[:4096]
	if root[4095] == '/' {
		root = root[:4095] + "q"
	}
	if len(root) != 4096 {
		t.Fatalf("fixture length = %d, want 4096", len(root))
	}
	for seg := range strings.SplitSeq(root[1:], "/") {
		if seg == "" || len(seg) > 255 {
			t.Fatalf("fixture has invalid path component of %d bytes", len(seg))
		}
	}
	return root
}

// TestLibraryProcessingRulesRootPathExclusion verifies that a full 4096-byte
// root_path is accepted and that library_processing_rules_library_path_excl
// rejects only exact (instance_id, root_path) duplicates.
func TestLibraryProcessingRulesRootPathExclusion(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	db := st.Pool()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	now := time.Now().UTC()
	instA := storetest.Instance(t, st, now)
	instB := storetest.Instance(t, st, now)
	root := libraryRoot4096(t)

	insert := func(instance uuid.UUID, path string) error {
		_, err := db.Exec(ctx, insertLibraryRootRule, uuid.NewString(), instance.String(), path, now)
		return err
	}

	if err := insert(instA, root); err != nil {
		t.Fatalf("insert 4096-byte root: %v", err)
	}

	err := insert(instA, root)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23P01" || pgErr.ConstraintName != libraryPathExclusion {
		t.Fatalf("duplicate insert error = %v, want SQLSTATE 23P01 on %s", err, libraryPathExclusion)
	}

	if err := insert(instB, root); err != nil {
		t.Fatalf("same root on alternate instance: %v", err)
	}

	last := byte('A')
	if root[len(root)-1] == last {
		last = 'B'
	}
	if err := insert(instA, root[:len(root)-1]+string(last)); err != nil {
		t.Fatalf("root differing in last byte: %v", err)
	}
}
