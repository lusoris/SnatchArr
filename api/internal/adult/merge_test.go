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
			Name:           "Kira Noir",
			Disambiguation: "",
			URLs:           []string{"https://twitter.com/kiranoir"},
			Links: []EntityLink{
				{InstanceID: inst1ID, ForeignID: "stashdb-performer-123"},
			},
		},
		{
			ID:             cand2ID,
			Name:           "Alex Adams",
			Disambiguation: "blonde",
			URLs:           []string{"https://www.iafd.com/person.rme/id=alex_adams/"},
			Links:          nil,
		},
	}

	cases := []struct {
		name       string
		incoming   IncomingEntity
		wantMatch  bool
		wantCandID uuid.UUID
	}{
		{
			name: "rule 1: match by endpoint URL pointing to known foreign ID",
			incoming: IncomingEntity{
				Name: "K. Noir",
				URLs: []string{"https://stashdb.org/performers/stashdb-performer-123"},
			},
			wantMatch:  true,
			wantCandID: cand1ID,
		},
		{
			name: "rule 2: match by normalized external site URL with different scheme and www",
			incoming: IncomingEntity{
				Name: "Different Name",
				URLs: []string{"http://www.twitter.com/kiranoir/"},
			},
			wantMatch:  true,
			wantCandID: cand1ID,
		},
		{
			name: "rule 3: exact normalized name when disambiguation empty on both",
			incoming: IncomingEntity{
				Name:           "kira noir",
				Disambiguation: "",
			},
			wantMatch:  true,
			wantCandID: cand1ID,
		},
		{
			name: "no false merge when disambiguation differs",
			incoming: IncomingEntity{
				Name:           "Alex Adams",
				Disambiguation: "brunette",
				URLs:           nil,
			},
			wantMatch: false,
		},
		{
			name: "no false merge when incoming disambiguation is non-empty and candidate is empty",
			incoming: IncomingEntity{
				Name:           "Kira Noir",
				Disambiguation: "parody",
				URLs:           nil,
			},
			wantMatch: false,
		},
		{
			name: "no false merge when candidate disambiguation is non-empty and incoming is empty",
			incoming: IncomingEntity{
				Name:           "Alex Adams",
				Disambiguation: "",
				URLs:           nil,
			},
			wantMatch: false,
		},
		{
			name: "no match when completely distinct",
			incoming: IncomingEntity{
				Name: "Unknown Performer",
				URLs: []string{"https://example.com/other"},
			},
			wantMatch: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matched, _ := MatchEntity(tc.incoming, candidates, endpoints)
			if tc.wantMatch {
				if matched == nil {
					t.Fatalf("expected match, got nil")
				}
				if matched.ID != tc.wantCandID {
					t.Errorf("matched ID = %v, want %v", matched.ID, tc.wantCandID)
				}
			} else if matched != nil {
				t.Fatalf("expected no match, got %v (%s)", matched.ID, matched.Name)
			}
		})
	}
}

func TestDeduplicateEntities(t *testing.T) {
	t.Parallel()
	id1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	id2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	id3 := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	items := []Candidate{
		{ID: id1, Name: "Studio Bravo", URLs: []string{"https://bravo.com"}},
		{ID: id2, Name: "Studio Bravo", URLs: []string{"http://www.bravo.com/"}},
		{ID: id3, Name: "Studio Charlie", URLs: []string{"https://charlie.com"}},
	}

	deduped := DeduplicateEntities(items, nil)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 deduped entities, got %d", len(deduped))
	}
	if deduped[0].Name != "Studio Bravo" || deduped[1].Name != "Studio Charlie" {
		t.Errorf("unexpected deduped names: %v, %v", deduped[0].Name, deduped[1].Name)
	}
}
