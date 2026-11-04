// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type ownershipFaultSource struct {
	*prioritySourceFake
	check func(context.Context, string) error
}

func (s ownershipFaultSource) UpsertPriority(ctx context.Context, client uuid.UUID, item string, priority int) error {
	if err := s.check(ctx, "priority"); err != nil {
		return err
	}
	return s.prioritySourceFake.UpsertPriority(ctx, client, item, priority)
}

func (s ownershipFaultSource) SetPaused(ctx context.Context, client uuid.UUID, item string) error {
	if err := s.check(ctx, "pause"); err != nil {
		return err
	}
	return s.prioritySourceFake.SetPaused(ctx, client, item)
}

func (s ownershipFaultSource) ClearPaused(ctx context.Context, client uuid.UUID, item string) error {
	if err := s.check(ctx, "resume"); err != nil {
		return err
	}
	return s.prioritySourceFake.ClearPaused(ctx, client, item)
}

type ownershipCompletionWriter struct {
	*fakePriorityWriter
	after func()
}

func (w ownershipCompletionWriter) SetItemPriority(ctx context.Context, client domain.DownloadClient, secret, item string, priority int) error {
	err := w.fakePriorityWriter.SetItemPriority(ctx, client, secret, item, priority)
	w.after()
	return err
}

func (w ownershipCompletionWriter) PauseItems(ctx context.Context, client domain.DownloadClient, secret string, ids []string) error {
	err := w.fakePriorityWriter.PauseItems(ctx, client, secret, ids)
	w.after()
	return err
}

func (w ownershipCompletionWriter) ResumeItems(ctx context.Context, client domain.DownloadClient, secret string, ids []string) error {
	err := w.fakePriorityWriter.ResumeItems(ctx, client, secret, ids)
	w.after()
	return err
}

func ownershipTestPlan(action string) domain.DownloadClientPlan {
	switch action {
	case "priority":
		return domain.DownloadClientPlan{Priority: []domain.PriorityWrite{{ID: "low", To: -1}}}
	case "pause":
		return domain.DownloadClientPlan{Pause: []string{"low"}}
	default:
		return domain.DownloadClientPlan{Resume: []string{"low"}}
	}
}

func runOwnershipWrite(ctx context.Context, r *Reconciler, action string, client domain.DownloadClient, own []PriorityOwnership) bool {
	plan := ownershipTestPlan(action)
	clients := map[string]uuid.UUID{"low": client.ID}
	switch action {
	case "priority":
		return r.executePriorityWrites(ctx, client, "", plan, clients)
	case "pause":
		return r.executePauseWrites(ctx, client, "", plan, clients)
	default:
		return r.executeResumeWrites(ctx, client, "", plan, clients, own)
	}
}

func TestReviewT4IndependentOwnershipContext(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"priority", "pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientSABnzbd, "act")
			client := inputs.Clients[0]
			own := []PriorityOwnership{{ClientID: client.ID, ItemID: "low", PausedBySnatchArr: action == "resume"}}
			ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
			defer cancel()
			checked := false
			source := ownershipFaultSource{prioritySourceFake: &prioritySourceFake{inputs: inputs}, check: func(ctx context.Context, _ string) error {
				checked = true
				if ctx.Err() != nil {
					return fmt.Errorf("cancelled ownership context: %w", ctx.Err())
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("ownership I/O has no deadline")
				}
				return nil
			}}
			writer := ownershipCompletionWriter{fakePriorityWriter: &fakePriorityWriter{}, after: cancel}
			r := newActTestReconciler(source, &PriorityObservation{}, writer)
			if !runOwnershipWrite(ctx, r, action, client, own) || !checked {
				t.Fatal("ownership persistence lost after cancellation")
			}
		})
	}
}

func TestReviewT4RepairAfterDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"priority", "pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			testOwnershipRepairAfterFailure(t, action)
		})
	}
}

func TestReviewT4PartialCancellation(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			testPartialOwnershipCancellation(t, action)
		})
	}
}

type prepareFaultSource struct {
	*prioritySourceFake
	failAt string
}

func (s prepareFaultSource) PrepareWrite(ctx context.Context, client uuid.UUID, item, action string, priority *int) error {
	if item == s.failAt {
		return errors.New("intent database unavailable")
	}
	return s.prioritySourceFake.PrepareWrite(ctx, client, item, action, priority)
}

func TestReviewT4IntentRequiredBeforeWrite(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"priority", "pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientSABnzbd, "act")
			client := inputs.Clients[0]
			source := prepareFaultSource{prioritySourceFake: &prioritySourceFake{inputs: inputs}, failAt: "low"}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(source, &PriorityObservation{}, writer)
			ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
			defer cancel()
			own := []PriorityOwnership{{ClientID: client.ID, ItemID: "low", PausedBySnatchArr: true}}
			if runOwnershipWrite(ctx, r, action, client, own) || len(writer.calls) != 0 {
				t.Fatalf("write without intent: %+v", writer.calls)
			}
		})
	}
}

func TestReviewT4RepairRequiresIntent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pending string
		state         domain.DownloadItemState
		wantOwned     bool
	}{
		{"no record", "", domain.DownloadItemPaused, false},
		{"pending pause matches", "pause", domain.DownloadItemPaused, true},
		{"pending pause not applied", "pause", domain.DownloadItemActive, false},
		{"user paused priority intent", "priority", domain.DownloadItemPaused, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			owner := inputs.Clients[1].ID
			own := PriorityOwnership{ClientID: owner, ItemID: "low", PendingAction: tc.pending}
			key := owner.String() + "\nlow"
			source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{key: own}}
			r := newActTestReconciler(source, &PriorityObservation{}, &fakePriorityWriter{})
			ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
			defer cancel()
			if !r.recoverPriorityOwnership(ctx, []PriorityItem{{ID: "low", State: tc.state}}, []PriorityOwnership{own}) {
				t.Fatal("repair failed")
			}
			if source.ownership[key].PausedBySnatchArr != tc.wantOwned {
				t.Fatalf("unexpected pause ownership: %+v", source.ownership[key])
			}
		})
	}
}

func TestReviewT4PartialIntentPreparation(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	client := inputs.Clients[0]
	source := prepareFaultSource{prioritySourceFake: &prioritySourceFake{}, failAt: "second"}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(source, &PriorityObservation{}, writer)
	ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
	defer cancel()
	plan := domain.DownloadClientPlan{Pause: []string{"first", "second"}}
	clients := map[string]uuid.UUID{"first": client.ID, "second": client.ID}
	if r.executePauseWrites(ctx, client, "", plan, clients) {
		t.Fatal("expected intent failure")
	}
	if len(writer.calls) != 0 || source.ownership[client.ID.String()+"\nfirst"].PendingAction != "" {
		t.Fatal("unwritten pause left pending intent")
	}
}

func testOwnershipRepairAfterFailure(t *testing.T, action string) {
	t.Helper()
	inputs := actTestInputs(domain.ClientSABnzbd, "act")
	client := inputs.Clients[1]
	key := client.ID.String() + "\nlow"
	initial := PriorityOwnership{ClientID: client.ID, ItemID: "low", PausedBySnatchArr: action == "resume"}
	base := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{key: initial}}
	broken := true
	source := ownershipFaultSource{prioritySourceFake: base, check: func(context.Context, string) error {
		if broken {
			return errors.New("database unavailable after remote success")
		}
		return nil
	}}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(source, &PriorityObservation{}, writer)
	ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
	defer cancel()
	if runOwnershipWrite(ctx, r, action, client, []PriorityOwnership{initial}) {
		t.Fatal("expected failed confirmation")
	}
	if len(writer.calls) != 1 || base.ownership[key].PendingAction != action {
		t.Fatalf("missing durable intent: %+v", base.ownership[key])
	}
	broken = false
	obs := PriorityObservation{Items: []PriorityItem{{ID: "low", Category: "tv", State: domain.DownloadItemPaused, Priority: -1}}}
	if action == "resume" {
		obs.Items[0].State = domain.DownloadItemActive
	}
	if action == "priority" {
		obs.Items[0].State = domain.DownloadItemQueued
	}
	// New reconciler proves recovery uses persisted state, not an in-memory retry.
	next := newActTestReconciler(source, &obs, &fakePriorityWriter{})
	if err := next.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRepairedOwnership(t, base.ownership[key], action)
}

func testPartialOwnershipCancellation(t *testing.T, action string) {
	t.Helper()
	inputs := actTestInputs(domain.ClientSABnzbd, "act")
	client := inputs.Clients[0]
	ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
	defer cancel()
	source := ownershipFaultSource{prioritySourceFake: &prioritySourceFake{}, check: func(ctx context.Context, _ string) error {
		if ctx.Err() != nil {
			return fmt.Errorf("record context: %w", ctx.Err())
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing ownership deadline")
		}
		return nil
	}}
	method := "PauseItems"
	if action == "resume" {
		method = "ResumeItems"
	}
	writer := ownershipCompletionWriter{fakePriorityWriter: &fakePriorityWriter{failMethod: method, failErr: errors.New("partial"), partialDone: []string{"low"}}, after: cancel}
	r := newActTestReconciler(source, &PriorityObservation{}, writer)
	own := []PriorityOwnership{{ClientID: client.ID, ItemID: "low", PausedBySnatchArr: true}}
	if runOwnershipWrite(ctx, r, action, client, own) {
		t.Fatal("partial error swallowed")
	}
	got := source.ownership[client.ID.String()+"\nlow"]
	if got.PendingAction != "" || got.PausedBySnatchArr != (action == "pause") {
		t.Fatalf("partial ownership lost: %+v", got)
	}
}

func assertRepairedOwnership(t *testing.T, got PriorityOwnership, action string) {
	t.Helper()
	if got.PendingAction != "" {
		t.Fatalf("intent not repaired next tick: %+v", got)
	}
	if action == "priority" && (got.LastWrittenPriority == nil || *got.LastWrittenPriority != -1) {
		t.Fatal("priority ownership not recovered")
	}
	if got.PausedBySnatchArr {
		t.Fatalf("pause ownership not released after successful recovery: %+v", got)
	}
}
