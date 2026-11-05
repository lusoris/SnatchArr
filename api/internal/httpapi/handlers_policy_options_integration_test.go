// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func TestGetPolicyFilterOptions(t *testing.T) {
	t.Parallel()
	t.Run("positive admin reads tags and profiles", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		f.dlDisc.labels = arrclient.Labels{
			Tags:     []arrclient.Label{{ID: 1, Name: "anime"}, {ID: 4, Name: "kids"}},
			Profiles: []arrclient.Label{{ID: 6, Name: "HD-1080p"}},
		}
		got, err := f.h.GetPolicyFilterOptions(withUser(t.Context(), f.admin), oas.GetPolicyFilterOptionsParams{InstanceId: instID})
		want := &oas.PolicyFilterOptions{
			Tags:            []oas.PolicyFilterOption{{ID: 1, Name: "anime"}, {ID: 4, Name: "kids"}},
			QualityProfiles: []oas.PolicyFilterOption{{ID: 6, Name: "HD-1080p"}},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("GetPolicyFilterOptions() = %+v, %v; want %+v", got, err, want)
		}
	})
	t.Run("boundary an instance without tags or profiles answers empty lists", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		got, err := f.h.GetPolicyFilterOptions(withUser(t.Context(), f.admin), oas.GetPolicyFilterOptionsParams{InstanceId: instID})
		if err != nil || got.Tags == nil || got.QualityProfiles == nil || len(got.Tags)+len(got.QualityProfiles) != 0 {
			t.Fatalf("GetPolicyFilterOptions() = %+v, %v; want two empty lists", got, err)
		}
	})
	cases := []struct {
		name    string
		user    func(f *integrationFixture) (domain.User, bool)
		unknown bool
		arrErr  error
		want    error
	}{
		{"negative no user", func(*integrationFixture) (domain.User, bool) { return domain.User{}, false }, false, nil, domain.ErrUnauthorized},
		{"negative viewer", func(f *integrationFixture) (domain.User, bool) { return f.viewer, true }, false, nil, domain.ErrForbidden},
		{"negative unknown instance", func(f *integrationFixture) (domain.User, bool) { return f.admin, true }, true, nil, domain.ErrNotFound},
		{
			"negative unreachable instance", func(f *integrationFixture) (domain.User, bool) { return f.admin, true }, false,
			fmt.Errorf("%w: tags: refused", domain.ErrUnreachable), domain.ErrUnreachable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			if tc.unknown {
				instID = uuid.New()
			}
			f.dlDisc.labelsErr = tc.arrErr
			ctx := t.Context()
			if u, ok := tc.user(f); ok {
				ctx = withUser(ctx, u)
			}
			if _, err := f.h.GetPolicyFilterOptions(ctx, oas.GetPolicyFilterOptionsParams{InstanceId: instID}); !errors.Is(err, tc.want) {
				t.Fatalf("GetPolicyFilterOptions() err = %v, want %v", err, tc.want)
			}
		})
	}
}

func seedRadarrInstance(t *testing.T, f *integrationFixture) uuid.UUID {
	t.Helper()
	inst, err := f.h.instances.Create(t.Context(), instances.Input{
		Kind:    domain.KindRadarr,
		Name:    "radarr-" + uuid.NewString(),
		BaseURL: "http://" + uuid.NewString() + ".invalid:7878",
		APIKey:  "secret-test-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("seedRadarrInstance: %v", err)
	}
	return inst.ID
}

func TestGetForeignSearchPreflight(t *testing.T) {
	t.Parallel()
	t.Run("positive admin reads radarr profiles and preflight verdicts", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedRadarrInstance(t, f)
		f.dlDisc.profileLangs = []arrclient.ProfileLanguage{
			{ID: 1, Name: "Any", Language: &foreigntitle.RadarrLanguage{ID: -1, Name: "Any"}},
			{ID: 2, Name: "NoLang", Language: nil},
		}
		got, err := f.h.GetForeignSearchPreflight(withUser(t.Context(), f.admin), oas.GetForeignSearchPreflightParams{InstanceId: instID})
		if err != nil {
			t.Fatalf("GetForeignSearchPreflight() error = %v", err)
		}
		if !got.Applies {
			t.Fatalf("GetForeignSearchPreflight() applies = false, want true")
		}
		if len(got.Languages) != 2 || got.Languages[0] != "de" || got.Languages[1] != "fr" {
			t.Fatalf("GetForeignSearchPreflight() languages = %v, want [de fr]", got.Languages)
		}
		if len(got.Profiles) != 2 {
			t.Fatalf("GetForeignSearchPreflight() profiles len = %d, want 2", len(got.Profiles))
		}
		p1 := got.Profiles[0]
		if p1.ID != 1 || p1.Name != "Any" || p1.ProfileLanguage.Null || p1.ProfileLanguage.Value.Name != "Any" {
			t.Fatalf("p1 = %+v, want valid RadarrLanguage", p1)
		}
		if len(p1.Verdicts) != 2 || p1.Verdicts[0].Verdict != oas.ForeignSearchVerdictVerdictAllowed {
			t.Fatalf("p1.Verdicts = %+v", p1.Verdicts)
		}
		p2 := got.Profiles[1]
		if p2.ID != 2 || p2.Name != "NoLang" || !p2.ProfileLanguage.Null {
			t.Fatalf("p2 = %+v, want null ProfileLanguage", p2)
		}
	})

	t.Run("positive non-radarr returns applies false and empty lists", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		got, err := f.h.GetForeignSearchPreflight(withUser(t.Context(), f.admin), oas.GetForeignSearchPreflightParams{InstanceId: instID})
		if err != nil {
			t.Fatalf("GetForeignSearchPreflight() error = %v", err)
		}
		if got.Applies {
			t.Fatalf("GetForeignSearchPreflight() applies = true, want false")
		}
		if got.Languages == nil || len(got.Languages) != 0 || got.Profiles == nil || len(got.Profiles) != 0 {
			t.Fatalf("GetForeignSearchPreflight() = %+v, want empty slices", got)
		}
	})

	cases := []struct {
		name    string
		user    func(f *integrationFixture) (domain.User, bool)
		unknown bool
		arrErr  error
		want    error
	}{
		{"negative no user", func(*integrationFixture) (domain.User, bool) { return domain.User{}, false }, false, nil, domain.ErrUnauthorized},
		{"negative viewer", func(f *integrationFixture) (domain.User, bool) { return f.viewer, true }, false, nil, domain.ErrForbidden},
		{"negative unknown instance", func(f *integrationFixture) (domain.User, bool) { return f.admin, true }, true, nil, domain.ErrNotFound},
		{
			"negative unreachable instance", func(f *integrationFixture) (domain.User, bool) { return f.admin, true }, false,
			fmt.Errorf("%w: upstream timeout", domain.ErrUnreachable), domain.ErrUnreachable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedRadarrInstance(t, f)
			if tc.unknown {
				instID = uuid.New()
			}
			f.dlDisc.profileLangsErr = tc.arrErr
			ctx := t.Context()
			if u, ok := tc.user(f); ok {
				ctx = withUser(ctx, u)
			}
			if _, err := f.h.GetForeignSearchPreflight(ctx, oas.GetForeignSearchPreflightParams{InstanceId: instID}); !errors.Is(err, tc.want) {
				t.Fatalf("GetForeignSearchPreflight() err = %v, want %v", err, tc.want)
			}
		})
	}
}
