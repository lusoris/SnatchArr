// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
)

// maxMigrationFiles bounds the directory listing (HISS-02): 500 versions, an up and a down each.
const maxMigrationFiles = 1000

// migrationName is the only file name the migrations directory may hold: a six-digit version, a
// lower snake case name and the direction.
var migrationName = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

// migrationPair is the name and the directions found for one version.
type migrationPair struct {
	name     string
	up, down bool
}

// LatestMigration checks the migrations in fsys and returns the highest version. Every version
// from 1 to the highest needs an up and a down file of the same name, and fsys holds nothing
// else. golang-migrate skips a file whose name it cannot parse and accepts a gap or a missing
// down file; each leaves a database that migrated without an error short of the schema the
// code expects, or one that cannot step back.
func LatestMigration(fsys fs.FS) (uint, error) {
	versions, err := readMigrations(fsys)
	if err != nil {
		return 0, err
	}
	var latest uint
	for v := range versions {
		latest = max(latest, v)
	}
	for v := uint(1); v <= latest; v++ {
		p, ok := versions[v]
		if !ok {
			return 0, fmt.Errorf("migration %06d missing: versions run 1 to %d without gaps", v, latest)
		}
		if !p.up || !p.down {
			return 0, fmt.Errorf("migration %06d_%s: needs both an up and a down file", v, p.name)
		}
	}
	return latest, nil
}

// readMigrations groups the files in fsys by version, refusing any file that is not a migration.
func readMigrations(fsys fs.FS) (map[uint]*migrationPair, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	if len(entries) == 0 || len(entries) > maxMigrationFiles {
		return nil, fmt.Errorf("%d migration files, expected 1 to %d", len(entries), maxMigrationFiles)
	}
	versions := make(map[uint]*migrationPair, len(entries)/2)
	for _, e := range entries {
		if err := addMigration(versions, e); err != nil {
			return nil, err
		}
	}
	return versions, nil
}

func addMigration(versions map[uint]*migrationPair, e fs.DirEntry) error {
	m := migrationName.FindStringSubmatch(e.Name())
	if e.IsDir() || m == nil {
		return fmt.Errorf("%s: not a migration file (want NNNNNN_name.up.sql or .down.sql)", e.Name())
	}
	v, err := strconv.ParseUint(m[1], 10, 32)
	if err != nil || v == 0 {
		return fmt.Errorf("%s: version must be 1 or above", e.Name())
	}
	p := versions[uint(v)]
	if p == nil {
		p = &migrationPair{name: m[2]}
		versions[uint(v)] = p
	}
	if p.name != m[2] {
		return fmt.Errorf("migration %s: named %q and %q", m[1], p.name, m[2])
	}
	if m[3] == "up" {
		p.up = true
	} else {
		p.down = true
	}
	return nil
}
