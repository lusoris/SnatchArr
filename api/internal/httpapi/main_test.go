// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const (
	// maxParallel caps t.Parallel tests when -test.parallel is not given. The go default is
	// GOMAXPROCS, which on a 32-core host ran about 32 fixtures at once and reached 10.85 GB
	// peak RSS under -race (measured 2026-10-29).
	maxParallel = 8
	// peakRSSBound fails the package when the process peak RSS exceeds it. Each argon2id hash
	// holds 64 MiB at production parameters; an unbounded fixture hash shows up here first. The
	// fixed package peaks near 660 MiB under -race (measured 2026-10-29).
	peakRSSBound = 1 << 30
)

// testHasher is the one password hasher every fixture shares: its slots bound the concurrent
// argon2id hashes across the package, and its parameters keep each hash at 1 MiB instead of
// the production 64 MiB. Login verifies with the parameters stored in the hash.
var testHasher = newCheapHasher(2)

// cheapHasher hashes with argon2id at test parameters, at most cap(sem) at a time.
type cheapHasher struct {
	sem  chan struct{}
	hash func(plain string) (string, error)
}

func newCheapHasher(slots int) *cheapHasher {
	p := *crypto.DefaultPasswordParams
	p.Memory, p.Iterations, p.Parallelism = 1024, 1, 1
	hash := func(plain string) (string, error) { return crypto.HashPasswordWith(plain, &p) }
	return &cheapHasher{sem: make(chan struct{}, slots), hash: hash}
}

// Hash implements auth.Hasher.
func (h *cheapHasher) Hash(ctx context.Context, plain string) (string, error) {
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
		return h.hash(plain)
	case <-ctx.Done():
		return "", fmt.Errorf("test hasher wait: %w", ctx.Err())
	}
}

// TestMain shares one Postgres container across the package's integration tests, bounds test
// parallelism and checks the peak RSS of the run.
func TestMain(m *testing.M) {
	flag.Parse()
	boundParallel(maxParallel)
	code := storetest.Main(m)
	if code == 0 {
		code = checkPeakRSS(peakRSSBound)
	}
	os.Exit(code)
}

// boundParallel lowers -test.parallel to limit unless the flag was given on the command line.
func boundParallel(limit int) {
	set := false
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == "test.parallel" })
	f := flag.Lookup("test.parallel")
	if set || f == nil {
		return
	}
	if cur, err := strconv.Atoi(f.Value.String()); err == nil && cur <= limit {
		return
	}
	if err := f.Value.Set(strconv.Itoa(limit)); err != nil {
		fmt.Fprintf(os.Stderr, "httpapi: bound test.parallel: %v\n", err)
	}
}

// checkPeakRSS returns 1 and reports when the peak RSS of this process exceeds bound.
func checkPeakRSS(bound int64) int {
	peak, ok := peakRSS()
	if !ok {
		fmt.Fprintln(os.Stderr, "httpapi: peak RSS check skipped: getrusage has no max RSS on this platform")
		return 0
	}
	fmt.Fprintf(os.Stderr, "httpapi: peak RSS %d MiB, bound %d MiB\n", peak>>20, bound>>20)
	if peak > bound {
		fmt.Fprintln(os.Stderr, "httpapi: FAIL: peak RSS exceeds bound")
		return 1
	}
	return 0
}
