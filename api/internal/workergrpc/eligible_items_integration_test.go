// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"math"
	"testing"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

func TestCompleteRunStoresEligibleItems(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sent *uint32
		want *int32
	}{
		{"positive", new(uint32(7)), new(int32(7))},
		{"zero", new(uint32(0)), new(int32(0))},
		{"absent", nil, nil},
		{"INTEGER overflow saturates", new(uint32(math.MaxUint32)), new(int32(math.MaxInt32))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newCompleteFixture(t)
			_, err := f.srv.CompleteRun(t.Context(), &snatcharrv1.CompleteRunRequest{
				RunId: f.run.ID.String(), WorkerId: "worker", Outcome: snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
				EligibleItems: tc.sent,
			})
			if err != nil {
				t.Fatalf("CompleteRun: %v", err)
			}
			row, err := f.st.Q().GetRun(t.Context(), f.run.ID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if (row.EligibleItems == nil) != (tc.want == nil) || (tc.want != nil && *row.EligibleItems != *tc.want) {
				t.Errorf("stored eligible_items = %v, want %v", row.EligibleItems, tc.want)
			}
		})
	}
}
