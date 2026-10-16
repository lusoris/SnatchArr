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
