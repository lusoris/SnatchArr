// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{in: "Jane Doe", want: "jane doe"},
		{in: "  JANE   DOE  ", want: "jane doe"},
		{in: "Studio   A\tB\nC", want: "studio a b c"},
		{in: "", want: ""},
	}
	for _, tc := range cases {
		if got := NormalizeName(tc.in); got != tc.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeExternalURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips www and trailing slash",
			in:   "https://www.twitter.com/JaneDoe/",
			want: "twitter.com/JaneDoe",
		},
		{
			name: "lowercases host but preserves path case",
			in:   "http://WWW.IAFD.com/person.rme/id=123",
			want: "iafd.com/person.rme/id=123",
		},
		{
			name: "preserves query parameters",
			in:   "https://instagram.com/janedoe/?hl=en",
			want: "instagram.com/janedoe?hl=en",
		},
		{
			name: "empty input",
			in:   "   ",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeExternalURL(tc.in)
			if err != nil {
				t.Fatalf("NormalizeExternalURL unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("NormalizeExternalURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMatchEntity(t *testing.T) {
	t.Parallel()
	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	endpoints := []StashboxEndpoint{
		{InstanceID: inst1ID, Endpoint: "https://stashdb.org/graphql"},
		{InstanceID: inst2ID, Endpoint: "https://alt-stash.org/graphql"},
	}

	cand1ID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	cand2ID := uuid.MustParse("20000000-0000-0000-0000-000000000002")

	candidates := []Candidate{
		{
			ID:             cand1ID,
			Name:           "Clara",
			Disambiguation: "",
			URLs:           []string{"https://social.example/clara_x12"},
			Links: []EntityLink{
				{InstanceID: inst1ID, ForeignID: "stashdb-performer-123"},
			},
		},
		{
			ID:             cand2ID,
			Name:           "Mila",
			Disambiguation: "studio-x-47",
			URLs:           []string{"https://www.index.example/person/id=mila_47/"},
			Links:          nil,
		},
	}

	cases := []struct {
		name       string
		incoming   IncomingEntity
		wantKind   MatchKind
		wantCandID uuid.UUID
	}{
		{
			name: "positive: match by endpoint URL pointing to known foreign ID",
			incoming: IncomingEntity{
				Name: "Clara 12",
				URLs: []string{"https://stashdb.org/performers/stashdb-performer-123"},
			},
			wantKind:   MatchAutoMerge,
			wantCandID: cand1ID,
		},
		{
			name: "positive: match by normalized external site URL with different scheme and www",
			incoming: IncomingEntity{
				Name: "Clara 12",
				URLs: []string{"http://www.social.example/clara_x12/"},
			},
			wantKind:   MatchAutoMerge,
			wantCandID: cand1ID,
		},
		{
			name: "negative: exact normalized name with empty disambiguation is audit candidate, not auto-merge",
			incoming: IncomingEntity{
				Name:           "clara",
				Disambiguation: "",
			},
			wantKind:   MatchAuditCandidate,
			wantCandID: cand1ID,
		},
		{
			name: "boundary: name differing only in case and spacing with no shared ID is audit candidate, not auto-merge",
			incoming: IncomingEntity{
				Name:           "  CLARA  ",
				Disambiguation: "",
			},
			wantKind:   MatchAuditCandidate,
			wantCandID: cand1ID,
		},
		{
			name: "boundary: empty name must not match",
			incoming: IncomingEntity{
				Name:           "   ",
				Disambiguation: "",
			},
			wantKind: MatchNone,
		},
		{
			name: "negative: no false merge when disambiguation differs",
			incoming: IncomingEntity{
				Name:           "Mila",
				Disambiguation: "studio-y-01",
				URLs:           nil,
			},
			wantKind: MatchNone,
		},
		{
			name: "negative: no false merge when incoming disambiguation is non-empty and candidate is empty",
			incoming: IncomingEntity{
				Name:           "Clara",
				Disambiguation: "studio-y",
				URLs:           nil,
			},
			wantKind: MatchNone,
		},
		{
			name: "negative: no false merge when candidate disambiguation is non-empty and incoming is empty",
			incoming: IncomingEntity{
				Name:           "Mila",
				Disambiguation: "",
				URLs:           nil,
			},
			wantKind: MatchNone,
		},
		{
			name: "negative: no match when completely distinct",
			incoming: IncomingEntity{
				Name: "Nora",
				URLs: []string{"https://example.com/other"},
			},
			wantKind: MatchNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matched, _, kind := MatchEntity(tc.incoming, candidates, endpoints)
			if kind != tc.wantKind {
				t.Fatalf("MatchEntity kind = %q, want %q", kind, tc.wantKind)
			}
			if tc.wantKind != MatchNone {
				if matched == nil {
					t.Fatalf("expected candidate for kind %q, got nil", tc.wantKind)
				}
				if matched.ID != tc.wantCandID {
					t.Errorf("matched ID = %v, want %v", matched.ID, tc.wantCandID)
				}
			} else if matched != nil {
				t.Fatalf("expected nil candidate for MatchNone, got %v (%s)", matched.ID, matched.Name)
			}
		})
	}
}

func TestDeduplicateEntities(t *testing.T) {
	t.Parallel()
	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	endpoints := []StashboxEndpoint{
		{InstanceID: inst1ID, Endpoint: "https://stashdb.org/graphql"},
		{InstanceID: inst2ID, Endpoint: "https://alt-stash.org/graphql"},
	}

	id1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	id2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	id3 := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	cases := []struct {
		name       string
		items      []Candidate
		wantCount  int
		wantLinks  []int
		wantAudits int
	}{
		{
			name: "negative: three performers with same first name (studio X 12 with number, studio X 47 bare first name, studio Y bare first name) keep three entities",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "Clara 12",
					URLs:  []string{"https://studio-x.example/models/clara-12"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-12"}},
				},
				{
					ID:    id2,
					Name:  "Clara",
					URLs:  []string{"https://studio-x.example/models/clara-47"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-47"}},
				},
				{
					ID:    id3,
					Name:  "Clara",
					URLs:  []string{"https://studio-y.example/models/clara"},
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "sb-studio-y-clara-01"}},
				},
			},
			wantCount:  3,
			wantLinks:  []int{1, 1, 1},
			wantAudits: 1,
		},
		{
			name: "negative: three performers with same bare first name (studio X 12, studio X 47, studio Y) and distinct foreign IDs keep three entities",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "Clara",
					URLs:  []string{"https://studio-x.example/models/clara-12"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-12"}},
				},
				{
					ID:    id2,
					Name:  "Clara",
					URLs:  []string{"https://studio-x.example/models/clara-47"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-47"}},
				},
				{
					ID:    id3,
					Name:  "Clara",
					URLs:  []string{"https://studio-y.example/models/clara"},
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "sb-studio-y-clara-01"}},
				},
			},
			wantCount:  3,
			wantLinks:  []int{1, 1, 1},
			wantAudits: 2,
		},
		{
			name: "positive: same foreign ID on two instances via endpoint URL cross-ref still merges",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "Clara 12",
					URLs:  []string{"https://studio-x.example/models/clara-12"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-12"}},
				},
				{
					ID:    id2,
					Name:  "Clara",
					URLs:  []string{"https://stashdb.org/performers/sb-studio-x-clara-12"},
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "alt-studio-x-clara-12"}},
				},
			},
			wantCount:  1,
			wantLinks:  []int{2},
			wantAudits: 0,
		},
		{
			name: "positive: same normalized external URL still merges",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "Studio Bravo",
					URLs:  []string{"https://bravo.example"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-bravo-1"}},
				},
				{
					ID:    id2,
					Name:  "Studio Bravo",
					URLs:  []string{"http://www.bravo.example/"},
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "sb-bravo-2"}},
				},
				{
					ID:    id3,
					Name:  "Studio Charlie",
					URLs:  []string{"https://charlie.example"},
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-charlie-1"}},
				},
			},
			wantCount:  2,
			wantLinks:  []int{2, 1},
			wantAudits: 0,
		},
		{
			name: "boundary: empty names with no shared ID are not merged and not audit candidates",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "",
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-empty-1"}},
				},
				{
					ID:    id2,
					Name:  "   ",
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "sb-empty-2"}},
				},
			},
			wantCount:  2,
			wantLinks:  []int{1, 1},
			wantAudits: 0,
		},
		{
			name: "boundary: names differing only in case and spacing with no shared ID are not merged and reported as audit candidate",
			items: []Candidate{
				{
					ID:    id1,
					Name:  "  CLARA   12 ",
					Links: []EntityLink{{InstanceID: inst1ID, ForeignID: "sb-studio-x-clara-12"}},
				},
				{
					ID:    id2,
					Name:  "clara 12",
					Links: []EntityLink{{InstanceID: inst2ID, ForeignID: "sb-studio-y-clara-12"}},
				},
			},
			wantCount:  2,
			wantLinks:  []int{1, 1},
			wantAudits: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deduped := DeduplicateEntities(tc.items, endpoints)
			if len(deduped) != tc.wantCount {
				t.Fatalf("expected %d deduped entities, got %d", tc.wantCount, len(deduped))
			}
			for i, wantL := range tc.wantLinks {
				if gotL := len(deduped[i].Links); gotL != wantL {
					t.Errorf("deduped[%d] (%q) links count = %d, want %d", i, deduped[i].Name, gotL, wantL)
				}
			}

			dedupedWithAudit, audits := DeduplicateEntitiesWithAudit(tc.items, endpoints)
			if len(dedupedWithAudit) != tc.wantCount {
				t.Fatalf("DeduplicateEntitiesWithAudit entities = %d, want %d", len(dedupedWithAudit), tc.wantCount)
			}
			if len(audits) != tc.wantAudits {
				t.Fatalf("DeduplicateEntitiesWithAudit audit candidates = %d, want %d", len(audits), tc.wantAudits)
			}
		})
	}
}
