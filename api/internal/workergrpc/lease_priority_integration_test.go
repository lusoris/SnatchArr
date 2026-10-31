// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func newPriorityFixture(t *testing.T) completeFixture {
	t.Helper()
	f := newCompleteFixture(t)
	if _, err := f.st.Q().EnsureSettings(t.Context(), f.clk.Now()); err != nil {
		t.Fatalf("EnsureSettings: %v", err)
	}
	if _, err := f.srv.runs.Complete(t.Context(), f.run.ID, "worker", domain.RunDone, 0, "", nil); err != nil {
		t.Fatalf("Complete initial fixture run: %v", err)
	}
	return f
}

func seedInstanceOfKind(t *testing.T, f completeFixture, kind domain.AppKind, name string, searchPrio *int) uuid.UUID {
	t.Helper()
	inst, err := f.srv.instances.Create(t.Context(), instances.Input{
		Kind:    kind,
		Name:    name,
		BaseURL: fmt.Sprintf("http://%s.invalid:8989", name),
		APIKey:  "a-key-for-the-priority-test",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("instances.Create(%s): %v", name, err)
	}
	if searchPrio != nil {
		if _, _, err := f.srv.instances.UpdatePriority(t.Context(), inst.ID, domain.InstancePriority{Search: searchPrio}); err != nil {
			t.Fatalf("instances.UpdatePriority(%s): %v", name, err)
		}
	}
	return inst.ID
}

func enqueuePriorityRun(t *testing.T, f completeFixture, instID uuid.UUID) domain.Run {
	t.Helper()
	run, ok, err := f.srv.runs.Enqueue(t.Context(), instID, domain.SnatchMissing)
	if err != nil || !ok {
		t.Fatalf("runs.Enqueue(%s): ok=%v, err=%v", instID, ok, err)
	}
	return run
}

func TestLeasePriority(t *testing.T) {
	t.Parallel()

	// Positive: Whisparr run queued first, Sonarr second -> Sonarr leased first.
	t.Run("positive preset rank leases sonarr before earlier whisparr", func(t *testing.T) {
		t.Parallel()
		f := newPriorityFixture(t)
		whisparrID := seedInstanceOfKind(t, f, domain.KindWhisparrV3, "whisparr-first", nil)
		sonarrID := seedInstanceOfKind(t, f, domain.KindSonarr, "sonarr-second", nil)

		runWhisparr := enqueuePriorityRun(t, f, whisparrID)
		f.clk.Advance(time.Second)
		runSonarr := enqueuePriorityRun(t, f, sonarrID)
		f.clk.Advance(time.Second)

		lease1, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil || lease1.GetRun() == nil {
			t.Fatalf("LeaseRun 1: run=%v, err=%v", lease1.GetRun(), err)
		}
		if lease1.GetRun().GetRunId() != runSonarr.ID.String() {
			t.Errorf("first leased run = %s, want Sonarr run %s", lease1.GetRun().GetRunId(), runSonarr.ID)
		}

		lease2, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-2"})
		if err != nil || lease2.GetRun() == nil {
			t.Fatalf("LeaseRun 2: run=%v, err=%v", lease2.GetRun(), err)
		}
		if lease2.GetRun().GetRunId() != runWhisparr.ID.String() {
			t.Errorf("second leased run = %s, want Whisparr run %s", lease2.GetRun().GetRunId(), runWhisparr.ID)
		}
	})

	// Boundary: equal rank -> queued_at order.
	t.Run("boundary equal rank leases in queued_at order", func(t *testing.T) {
		t.Parallel()
		f := newPriorityFixture(t)
		radarrID := seedInstanceOfKind(t, f, domain.KindRadarr, "radarr-equal-1", nil)
		sonarrID := seedInstanceOfKind(t, f, domain.KindSonarr, "sonarr-equal-2", nil)

		runRadarr := enqueuePriorityRun(t, f, radarrID)
		f.clk.Advance(time.Second)
		runSonarr := enqueuePriorityRun(t, f, sonarrID)
		f.clk.Advance(time.Second)

		lease1, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil || lease1.GetRun() == nil {
			t.Fatalf("LeaseRun 1: run=%v, err=%v", lease1.GetRun(), err)
		}
		if lease1.GetRun().GetRunId() != runRadarr.ID.String() {
			t.Errorf("first leased run = %s, want earlier equal-rank Radarr run %s", lease1.GetRun().GetRunId(), runRadarr.ID)
		}

		lease2, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-2"})
		if err != nil || lease2.GetRun() == nil {
			t.Fatalf("LeaseRun 2: run=%v, err=%v", lease2.GetRun(), err)
		}
		if lease2.GetRun().GetRunId() != runSonarr.ID.String() {
			t.Errorf("second leased run = %s, want later equal-rank Sonarr run %s", lease2.GetRun().GetRunId(), runSonarr.ID)
		}
	})

	// Boundary: Whisparr run older than max_wait overtakes Sonarr.
	t.Run("boundary starved whisparr older than max_wait overtakes sonarr", func(t *testing.T) {
		t.Parallel()
		f := newPriorityFixture(t)
		whisparrID := seedInstanceOfKind(t, f, domain.KindWhisparrV3, "whisparr-starved", nil)
		sonarrID := seedInstanceOfKind(t, f, domain.KindSonarr, "sonarr-fresh", nil)

		runWhisparr := enqueuePriorityRun(t, f, whisparrID)
		f.clk.Advance(time.Hour)
		runSonarr := enqueuePriorityRun(t, f, sonarrID)
		f.clk.Advance(time.Second)

		lease1, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil || lease1.GetRun() == nil {
			t.Fatalf("LeaseRun 1: run=%v, err=%v", lease1.GetRun(), err)
		}
		if lease1.GetRun().GetRunId() != runWhisparr.ID.String() {
			t.Errorf("first leased run = %s, want starved Whisparr run %s (not Sonarr %s)", lease1.GetRun().GetRunId(), runWhisparr.ID, runSonarr.ID)
		}
	})

	// Positive: override search_priority=1 on whisparr wins.
	t.Run("positive search_priority override 1 on whisparr wins over sonarr", func(t *testing.T) {
		t.Parallel()
		f := newPriorityFixture(t)
		sonarrID := seedInstanceOfKind(t, f, domain.KindSonarr, "sonarr-default", nil)
		whisparrID := seedInstanceOfKind(t, f, domain.KindWhisparrV3, "whisparr-override", new(1))

		_ = enqueuePriorityRun(t, f, sonarrID)
		f.clk.Advance(time.Second)
		runWhisparr := enqueuePriorityRun(t, f, whisparrID)
		f.clk.Advance(time.Second)

		lease1, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil || lease1.GetRun() == nil {
			t.Fatalf("LeaseRun 1: run=%v, err=%v", lease1.GetRun(), err)
		}
		if lease1.GetRun().GetRunId() != runWhisparr.ID.String() {
			t.Errorf("first leased run = %s, want overridden Whisparr run %s", lease1.GetRun().GetRunId(), runWhisparr.ID)
		}
	})

	// Boundary: migration 000025 seeds the settings row; with the row deleted the preset rank is
	// NULL for every kind, so the LEFT JOIN degrades to queued_at order instead of failing.
	t.Run("boundary no settings row leases in queued_at order", func(t *testing.T) {
		t.Parallel()
		f := newCompleteFixture(t)
		if _, err := f.srv.runs.Complete(t.Context(), f.run.ID, "worker", domain.RunDone, 0, "", nil); err != nil {
			t.Fatalf("Complete initial fixture run: %v", err)
		}
		if _, err := f.st.Pool().Exec(t.Context(), "DELETE FROM settings"); err != nil {
			t.Fatalf("delete settings row: %v", err)
		}
		whisparrID := seedInstanceOfKind(t, f, domain.KindWhisparrV3, "whisparr-first-nosettings", nil)
		sonarrID := seedInstanceOfKind(t, f, domain.KindSonarr, "sonarr-second-nosettings", nil)

		runWhisparr := enqueuePriorityRun(t, f, whisparrID)
		f.clk.Advance(time.Second)
		runSonarr := enqueuePriorityRun(t, f, sonarrID)
		f.clk.Advance(time.Second)

		lease1, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil || lease1.GetRun() == nil {
			t.Fatalf("LeaseRun 1: run=%v, err=%v", lease1.GetRun(), err)
		}
		if lease1.GetRun().GetRunId() != runWhisparr.ID.String() {
			t.Errorf("first leased run = %s, want Whisparr run %s in queued_at order when settings row is missing", lease1.GetRun().GetRunId(), runWhisparr.ID)
		}

		lease2, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-2"})
		if err != nil || lease2.GetRun() == nil {
			t.Fatalf("LeaseRun 2: run=%v, err=%v", lease2.GetRun(), err)
		}
		if lease2.GetRun().GetRunId() != runSonarr.ID.String() {
			t.Errorf("second leased run = %s, want Sonarr run %s in queued_at order when settings row is missing", lease2.GetRun().GetRunId(), runSonarr.ID)
		}
	})

	// Negative: empty queue returns nil Run.
	t.Run("negative no queued runs returns nil run", func(t *testing.T) {
		t.Parallel()
		f := newPriorityFixture(t)
		lease, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "w-1"})
		if err != nil {
			t.Fatalf("LeaseRun: %v", err)
		}
		if lease.GetRun() != nil {
			t.Errorf("LeaseRun Run = %v on empty queue, want nil", lease.GetRun())
		}
	})
}
