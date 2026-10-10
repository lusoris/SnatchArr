// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	dbpgx "github.com/golusoris/golusoris/db/pgx"
)

const baseDSN = "postgres://snatcharr@db.invalid:5432/snatcharr?sslmode=require"

// query parses dsn and returns its query parameters and password.
func query(t *testing.T, dsn string) (url.Values, string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	password, _ := u.User.Password()
	return u.Query(), password
}

func TestMigratorDSNAppliesSSL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dsn  string
		ssl  dbpgx.SSLOptions
		want map[string]string
	}{
		{
			"positive: every field set", baseDSN,
			dbpgx.SSLOptions{Mode: "verify-full", RootCert: "/ca/ca.crt", Cert: "/tls/tls.crt", Key: "/tls/tls.key"},
			map[string]string{"sslmode": "verify-full", "sslrootcert": "/ca/ca.crt", "sslcert": "/tls/tls.crt", "sslkey": "/tls/tls.key"},
		},
		{
			"positive: postgresql scheme", "postgresql://snatcharr@db.invalid/snatcharr",
			dbpgx.SSLOptions{RootCert: "/ca/ca.crt"},
			map[string]string{"sslrootcert": "/ca/ca.crt"},
		},
		{
			"negative: nothing set keeps the DSN's own", baseDSN,
			dbpgx.SSLOptions{},
			map[string]string{"sslmode": "require", "sslrootcert": ""},
		},
		{
			"boundary: only the root cert keeps the DSN's mode", baseDSN,
			dbpgx.SSLOptions{RootCert: "/ca/ca.crt"},
			map[string]string{"sslmode": "require", "sslrootcert": "/ca/ca.crt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := migratorDSN(dbpgx.Options{DSN: tc.dsn, SSL: tc.ssl})
			if err != nil {
				t.Fatalf("migratorDSN() = %v", err)
			}
			q, _ := query(t, got)
			for k, v := range tc.want {
				if q.Get(k) != v {
					t.Errorf("%s = %q, want %q (dsn %s)", k, q.Get(k), v, got)
				}
			}
		})
	}
}

// Boundary: a keyword/value DSN is not a URL; it goes through unchanged for golusoris to judge.
func TestMigratorDSNKeepsKeywordDSN(t *testing.T) {
	t.Parallel()
	const dsn = "host=db.invalid user=snatcharr dbname=snatcharr"
	got, err := migratorDSN(dbpgx.Options{DSN: dsn, SSL: dbpgx.SSLOptions{Mode: "verify-full"}})
	if err != nil || got != dsn {
		t.Fatalf("migratorDSN() = %q, %v; want the DSN unchanged", got, err)
	}
}

func TestMigratorDSNReadsPasswordFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "password")
	const secret = "p@ss:w/rd %"
	if err := os.WriteFile(file, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := migratorDSN(dbpgx.Options{DSN: baseDSN, PasswordFile: file})
	if err != nil {
		t.Fatalf("migratorDSN() = %v", err)
	}
	if _, password := query(t, got); password != secret {
		t.Fatalf("password = %q, want %q (trailing newline trimmed, special characters kept)", password, secret)
	}
	if _, err := migratorDSN(dbpgx.Options{DSN: baseDSN, PasswordFile: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("migratorDSN() with a missing password file = nil error")
	}
}

// pgx honours the merged parameters: a root CA that does not exist fails the parse, which the
// bare DSN does not (the failure the canary hit was the CA being ignored).
func TestMigratorDSNReachesPgx(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "absent-ca.crt")
	if _, err := pgx.ParseConfig(baseDSN); err != nil {
		t.Fatalf("ParseConfig(bare DSN) = %v", err)
	}
	got, err := migratorDSN(dbpgx.Options{DSN: baseDSN, SSL: dbpgx.SSLOptions{Mode: "verify-full", RootCert: missing}})
	if err != nil {
		t.Fatalf("migratorDSN() = %v", err)
	}
	if _, err := pgx.ParseConfig(got); err == nil {
		t.Fatalf("ParseConfig(%s) accepted a root CA that does not exist", got)
	}
}

// An explicit db.migrate.dsn is the operator's and stays as given.
func TestEmbedMigrationsKeepsExplicitDSN(t *testing.T) {
	t.Parallel()
	pgxOpts := dbpgx.Options{DSN: baseDSN, SSL: dbpgx.SSLOptions{RootCert: "/ca/ca.crt"}}
	const explicit = "postgres://migrator@db.invalid/snatcharr"
	got, err := embedMigrations(dbmigrate.Options{DSN: explicit}, pgxOpts)
	if err != nil || got.DSN != explicit || !got.Auto {
		t.Fatalf("embedMigrations(explicit) = %q auto=%v, %v", got.DSN, got.Auto, err)
	}
	got, err = embedMigrations(dbmigrate.Options{}, pgxOpts)
	if q, _ := query(t, got.DSN); err != nil || q.Get("sslrootcert") != "/ca/ca.crt" {
		t.Fatalf("embedMigrations(default) = %q, %v; want the pool DSN with sslrootcert", got.DSN, err)
	}
}
