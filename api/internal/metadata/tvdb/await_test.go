// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

// TestAwaitTakesQueuedResultAfterCancel: the worker queues its result and then cancels lookupCtx.
// select picks among ready cases at random, so 64 rounds leave a missing check a 2^-64 chance to pass.
func TestAwaitTakesQueuedResultAfterCancel(t *testing.T) {
	t.Parallel()
	want := []foreigntitle.LanguageTitle{{Language: "de", Title: "Breaking Bad DE", Source: "tvdb"}}
	for round := range 64 {
		lookupCtx, cancel := context.WithCancel(t.Context())
		result := make(chan lookupResult, 1)
		result <- lookupResult{titles: want}
		cancel()
		got, err := (&Provider{}).await(t.Context(), lookupCtx, result)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("round %d: await() = %v, %v, want %v, nil", round, got, err, want)
		}
	}
}

type awaitCase struct {
	queued       *lookupResult
	name         string
	wantErrs     []error
	cancelCaller bool
	cancelLookup bool
	expireLookup bool
}

func TestAwaitStopReasons(t *testing.T) {
	t.Parallel()
	errQueued := errors.New("queued lookup error")
	for _, tc := range []awaitCase{
		{name: "queued error after cancel", cancelLookup: true, queued: &lookupResult{err: errQueued}, wantErrs: []error{errQueued}},
		{name: "lookup cancelled, nothing queued", cancelLookup: true, wantErrs: []error{ErrUnavailable, context.Canceled}},
		{name: "lookup deadline passed, nothing queued", expireLookup: true, wantErrs: []error{ErrUnavailable, context.DeadlineExceeded}},
		{name: "caller cancelled, nothing queued", cancelCaller: true, wantErrs: []error{ErrUnavailable, context.Canceled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runAwaitCase(t, tc)
		})
	}
}

func runAwaitCase(t *testing.T, tc awaitCase) {
	t.Helper()
	caller, cancelCaller := context.WithCancel(t.Context())
	defer cancelCaller()
	lookupCtx, cancelLookup := context.WithCancel(t.Context())
	defer cancelLookup()
	if tc.expireLookup {
		lookupCtx, cancelLookup = context.WithDeadline(t.Context(), time.Unix(1, 0))
		defer cancelLookup()
	}
	result := make(chan lookupResult, 1)
	if tc.queued != nil {
		result <- *tc.queued
	}
	if tc.cancelCaller {
		cancelCaller()
	}
	if tc.cancelLookup {
		cancelLookup()
	}
	got, err := (&Provider{}).await(caller, lookupCtx, result)
	if got != nil {
		t.Fatalf("await() titles = %v, want none", got)
	}
	for _, want := range tc.wantErrs {
		if !errors.Is(err, want) {
			t.Fatalf("await() error = %v, want errors.Is %v", err, want)
		}
	}
}
