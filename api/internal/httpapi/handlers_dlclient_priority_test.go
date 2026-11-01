// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type priorityPlanReaderFake struct {
	plan dlclients.PriorityPlan
	err  error
}

func (f priorityPlanReaderFake) Latest(id uuid.UUID) (dlclients.PriorityPlan, error) {
	if id != f.plan.ClientID {
		return dlclients.PriorityPlan{}, domain.ErrNotFound
	}
	return f.plan, f.err
}

func TestGetDownloadClientPriorityPlan(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	for _, tc := range []struct {
		name      string
		reader    priorityPlanReader
		id        uuid.UUID
		wantError bool
	}{
		{"not observed", nil, clientID, true},
		{"unknown client", priorityPlanReaderFake{}, clientID, true},
		{"empty plan", priorityPlanReaderFake{plan: dlclients.PriorityPlan{ClientID: clientID, Mode: "observe", ObservedAt: clock.NewFake().Now()}}, clientID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &Handlers{priorityPlans: tc.reader}
			got, err := h.GetDownloadClientPriorityPlan(t.Context(), oas.GetDownloadClientPriorityPlanParams{ClientId: tc.id})
			if tc.wantError {
				if !errors.Is(err, domain.ErrNotFound) || h.NewError(t.Context(), err).StatusCode != http.StatusNotFound {
					t.Fatalf("response = %+v, err = %v", got, err)
				}
				return
			}
			if err != nil || got.ClientId != clientID || got.Mode != oas.DownloadClientPriorityModeObserve || len(got.Priority)+len(got.Order)+len(got.Pause)+len(got.Resume) != 0 {
				t.Fatalf("response = %+v, err = %v", got, err)
			}
		})
	}
}

func TestPriorityPlanToOAS(t *testing.T) {
	t.Parallel()
	plan := dlclients.PriorityPlan{
		ClientID: uuid.New(), Mode: "act", ObservedAt: clock.NewFake().Now(), QueueingDisabled: true, Unattributed: 3,
		Plan: domain.DownloadClientPlan{Priority: []domain.PriorityWrite{{ID: "id", From: 0, To: 1}}, Pause: []string{"pause"}, Resume: []string{"resume"}},
	}
	got := priorityPlanToOAS(plan)
	if got.ClientId != plan.ClientID || string(got.Mode) != plan.Mode || !got.ObservedAt.Equal(plan.ObservedAt) || !got.QueueingDisabled || got.Unattributed != 3 || !reflect.DeepEqual(got.Priority, []oas.DownloadClientPriorityMove{{ID: "id", From: 0, To: 1}}) {
		t.Fatalf("response = %+v", got)
	}
	got.Pause[0] = "changed"
	if plan.Plan.Pause[0] != "pause" {
		t.Fatal("response aliases cached plan")
	}
}
