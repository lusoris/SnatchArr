// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package overlay

import (
	"maps"
	"reflect"
	"runtime"
	"testing"

	"github.com/golusoris/goenvoy/arr/whisparr"
)

func TestDuplicateMovieIDsAllocationAndSet(t *testing.T) {
	// t.Setenv marks this top-level test non-parallel so sibling tests do not skew TotalAlloc.
	t.Setenv("SNATCHARR_OVERLAY_SERIAL_MEMSTATS", "1")
	const (
		movieCount    = 50_000
		maxAllocBytes = 8*movieCount + 65_536
	)
	movies := make([]whisparr.Movie, movieCount)
	for index := range movies {
		movies[index] = whisparr.Movie{ID: movieCount - index, HasFile: true}
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	gotUnique := duplicateMovieIDs(movies)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(movies)
	allocBytes := after.TotalAlloc - before.TotalAlloc
	t.Logf("duplicateMovieIDs TotalAlloc delta = %d bytes; whisparr.Movie size = %d bytes", allocBytes, reflect.TypeFor[whisparr.Movie]().Size())
	if allocBytes > maxAllocBytes {
		t.Fatalf("duplicateMovieIDs TotalAlloc delta = %d bytes, want <= %d", allocBytes, maxAllocBytes)
	}
	if len(gotUnique) != 0 {
		t.Fatalf("duplicateMovieIDs unique = %v, want empty set", gotUnique)
	}
	repeated := []whisparr.Movie{
		{ID: 1, HasFile: true},
		{ID: 2, HasFile: true},
		{ID: 2, HasFile: true},
		{ID: 3, HasFile: true},
		{ID: 3, HasFile: true},
		{ID: 3, HasFile: true},
	}
	gotRepeated := duplicateMovieIDs(repeated)
	wantRepeated := map[int]struct{}{2: {}, 3: {}}
	if !maps.Equal(gotRepeated, wantRepeated) {
		t.Fatalf("duplicateMovieIDs repeated = %v, want %v", gotRepeated, wantRepeated)
	}
}
