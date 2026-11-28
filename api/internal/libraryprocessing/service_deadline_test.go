// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// contractOperationTimeout is the public API contract for the default operation
// deadline. It is intentionally a literal owned by this test, so the test can be
// replayed against revisions of service.go that predate the production constant.
const contractOperationTimeout = 5 * time.Second

// recordingChecker captures the context handed to the module check. It reports
// the module as disabled so no store access happens (the store is nil).
type recordingChecker struct {
	ctx         context.Context
	deadline    time.Time
	hasDeadline bool
	calls       int
}

// IsEnabled records the context it receives and returns that context's error,
// or false with no error when the context is still live.
func (r *recordingChecker) IsEnabled(ctx context.Context, _ string) (bool, error) {
	r.calls++
	r.ctx = ctx
	r.deadline, r.hasDeadline = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, nil
}

// serviceCall names one Service entry point and invokes it, keeping only the error.
type serviceCall struct {
	name string
	call func(s *Service, ctx context.Context) error
}

// parentCase describes a parent context and the expectations for the operation
// context derived from it. A nil window means the call is expected to fail with
// wantErr before any deadline expectation applies.
type parentCase struct {
	name    string
	newCtx  func() (context.Context, context.CancelFunc)
	wantErr error
	window  func(parent context.Context, before, after time.Time) (lo, hi time.Time)
}

// serviceCalls returns all eight Service entry points that apply an operation deadline.
func serviceCalls() []serviceCall {
	return []serviceCall{
		{"Create", func(s *Service, ctx context.Context) error {
			_, err := s.Create(ctx, RuleInput{})
			return err
		}},
		{"Get", func(s *Service, ctx context.Context) error {
			_, err := s.Get(ctx, uuid.Nil)
			return err
		}},
		{"List", func(s *Service, ctx context.Context) error {
			_, err := s.List(ctx)
			return err
		}},
		{"Update", func(s *Service, ctx context.Context) error {
			_, err := s.Update(ctx, uuid.Nil, RuleInput{})
			return err
		}},
		{"Delete", func(s *Service, ctx context.Context) error {
			return s.Delete(ctx, uuid.Nil)
		}},
		{"Resolve", func(s *Service, ctx context.Context) error {
			_, err := s.Resolve(ctx, ResolveQuery{})
			return err
		}},
		{"ListPage", func(s *Service, ctx context.Context) error {
			_, err := s.ListPage(ctx, PageQuery{})
			return err
		}},
		{"StreamAll", func(s *Service, ctx context.Context) error {
			return s.StreamAll(ctx, 0, func([]StoredRule) error { return nil })
		}},
	}
}

// timeoutParent returns a constructor for a parent context with the given timeout.
func timeoutParent(d time.Duration) func() (context.Context, context.CancelFunc) {
	return func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), d)
	}
}

// defaultWindow returns the accepted deadline range when the service applies its
// own default timeout, bracketed by timestamps taken around the call.
func defaultWindow(_ context.Context, before, after time.Time) (time.Time, time.Time) {
	return before.Add(contractOperationTimeout), after.Add(contractOperationTimeout)
}

// inheritedWindow returns the parent's own deadline as the only accepted value,
// for parents whose deadline is shorter than the default timeout.
func inheritedWindow(parent context.Context, _, _ time.Time) (time.Time, time.Time) {
	pd, _ := parent.Deadline()
	return pd, pd
}

// parentCases returns the parent context scenarios exercised for every entry point.
func parentCases() []parentCase {
	return []parentCase{
		{name: "no parent deadline", window: defaultWindow, newCtx: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
		{name: "looser parent deadline", newCtx: timeoutParent(time.Hour), window: defaultWindow},
		{name: "shorter parent deadline", newCtx: timeoutParent(2 * time.Second), window: inheritedWindow},
		{name: "cancelled parent", wantErr: context.Canceled, newCtx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}},
		{name: "expired parent deadline", wantErr: context.DeadlineExceeded, newCtx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
		}},
	}
}

// assertDeadlineWindow fails the test unless the recorded deadline exists and
// lies within the window computed for the parent case.
func assertDeadlineWindow(t *testing.T, rec *recordingChecker, pc parentCase, parent context.Context, before, after time.Time) {
	t.Helper()
	if !rec.hasDeadline {
		t.Fatal("context passed to module check has no deadline")
	}
	lo, hi := pc.window(parent, before, after)
	if rec.deadline.Before(lo) || rec.deadline.After(hi) {
		t.Fatalf("deadline %v outside expected window [%v, %v]", rec.deadline, lo, hi)
	}
}

// runDeadlineCase invokes one entry point with one parent context and verifies
// error propagation, the derived deadline, and release of the operation context.
func runDeadlineCase(t *testing.T, sc serviceCall, pc parentCase) {
	t.Helper()
	parent, cancel := pc.newCtx()
	defer cancel()

	rec := &recordingChecker{}
	svc := NewServiceWithChecker(nil, nil, nil, rec)

	before := time.Now()
	err := sc.call(svc, parent)
	after := time.Now()

	if rec.calls != 1 {
		t.Fatalf("module checker calls = %d, want 1", rec.calls)
	}
	want := pc.wantErr
	if want == nil {
		want = domain.ErrNotFound
	}
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want errors.Is %v", err, want)
	}
	if pc.window != nil {
		assertDeadlineWindow(t, rec, pc, parent, before, after)
	}
	if rec.ctx.Err() == nil {
		t.Fatal("operation context not cancelled after method returned")
	}
}

// TestServiceOperationDeadline verifies that every Service entry point bounds its
// work by the five-second contract, honours shorter parent deadlines and
// cancellation, and releases the operation context on return.
func TestServiceOperationDeadline(t *testing.T) {
	t.Parallel()
	for _, sc := range serviceCalls() {
		for _, pc := range parentCases() {
			t.Run(sc.name+"/"+pc.name, func(t *testing.T) {
				t.Parallel()
				runDeadlineCase(t, sc, pc)
			})
		}
	}
}
