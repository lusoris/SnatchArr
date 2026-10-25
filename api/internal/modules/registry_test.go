// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
)

func dummyValidator(json.RawMessage) error { return nil }

func TestRegistryValidation(t *testing.T) {
	t.Parallel()

	buildN := func(n int) []modules.Descriptor {
		res := make([]modules.Descriptor, n)
		for i := range n {
			res[i] = modules.Descriptor{
				ID:             fmt.Sprintf("mod_%02d", i),
				DefaultEnabled: false,
				ValidateConfig: dummyValidator,
			}
		}
		return res
	}

	cases := []struct {
		name        string
		descriptors []modules.Descriptor
		wantErr     error
	}{
		{
			name:        "positive valid built-ins",
			descriptors: modules.DefaultDescriptors(),
			wantErr:     nil,
		},
		{
			name:        "boundary 32 modules accepted",
			descriptors: buildN(32),
			wantErr:     nil,
		},
		{
			name:        "negative 33 modules refused",
			descriptors: buildN(33),
			wantErr:     domain.ErrInvalid,
		},
		{
			name: "negative duplicate id",
			descriptors: []modules.Descriptor{
				{ID: domain.ModuleLLM, ValidateConfig: dummyValidator},
				{ID: domain.ModuleLLM, ValidateConfig: dummyValidator},
			},
			wantErr: domain.ErrConflict,
		},
		{
			name: "negative empty id",
			descriptors: []modules.Descriptor{
				{ID: "", ValidateConfig: dummyValidator},
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative bad id starting with digit",
			descriptors: []modules.Descriptor{
				{ID: "1module", ValidateConfig: dummyValidator},
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative bad id with uppercase",
			descriptors: []modules.Descriptor{
				{ID: "LLM", ValidateConfig: dummyValidator},
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative bad id with hyphen",
			descriptors: []modules.Descriptor{
				{ID: "mod-hyphen", ValidateConfig: dummyValidator},
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative nil validator",
			descriptors: []modules.Descriptor{
				{ID: "mod_valid", ValidateConfig: nil},
			},
			wantErr: domain.ErrInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, err := modules.NewRegistry(tc.descriptors)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("NewRegistry() error = %v, want nil", err)
				}
				if reg == nil {
					t.Fatal("NewRegistry() returned nil registry")
				}
				ids := reg.IDs()
				if !slices.IsSorted(ids) {
					t.Fatalf("reg.IDs() = %v is not sorted", ids)
				}
				descs := reg.Descriptors()
				if len(descs) != len(ids) {
					t.Fatalf("reg.Descriptors() len = %d, want %d", len(descs), len(ids))
				}
				for i, d := range descs {
					if d.ID != ids[i] {
						t.Fatalf("reg.Descriptors()[%d].ID = %q, want %q", i, d.ID, ids[i])
					}
				}
				if len(descs) > 0 {
					origID := descs[0].ID
					descs[0].ID = "mutated"
					if reg.Descriptors()[0].ID != origID {
						t.Fatalf("reg.Descriptors() returned mutable internal slice")
					}
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewRegistry() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		val     func(json.RawMessage) error
		raw     string
		wantErr bool
	}{
		{name: "positive llm empty object ok", val: modules.ValidateLLMConfig, raw: "{}", wantErr: false},
		{name: "negative llm unknown field refused", val: modules.ValidateLLMConfig, raw: `{"x":1}`, wantErr: true},
		{name: "negative llm null refused", val: modules.ValidateLLMConfig, raw: "null", wantErr: true},
		{name: "negative llm array refused", val: modules.ValidateLLMConfig, raw: "[]", wantErr: true},
		{name: "negative llm string refused", val: modules.ValidateLLMConfig, raw: `"str"`, wantErr: true},
		{name: "negative llm empty raw refused", val: modules.ValidateLLMConfig, raw: "", wantErr: true},
		{name: "positive watchaware empty object ok", val: modules.ValidateWatchAwareConfig, raw: "{}", wantErr: false},
		{name: "positive watchaware boundary 5 ok", val: modules.ValidateWatchAwareConfig, raw: `{"sync_interval_minutes":5}`, wantErr: false},
		{name: "positive watchaware boundary 1440 ok", val: modules.ValidateWatchAwareConfig, raw: `{"sync_interval_minutes":1440}`, wantErr: false},
		{name: "negative watchaware boundary 4 refused", val: modules.ValidateWatchAwareConfig, raw: `{"sync_interval_minutes":4}`, wantErr: true},
		{name: "negative watchaware boundary 1441 refused", val: modules.ValidateWatchAwareConfig, raw: `{"sync_interval_minutes":1441}`, wantErr: true},
		{name: "negative watchaware string value refused", val: modules.ValidateWatchAwareConfig, raw: `{"sync_interval_minutes":"10"}`, wantErr: true},
		{name: "negative watchaware unknown key refused", val: modules.ValidateWatchAwareConfig, raw: `{"other_key":10}`, wantErr: true},
		{name: "negative watchaware null refused", val: modules.ValidateWatchAwareConfig, raw: "null", wantErr: true},
		{name: "positive watchaware gate empty object ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{}}`, wantErr: false},
		{name: "positive watchaware gate defaults ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"enabled":true,"mode":"observe","seasons_on_add":1,"look_ahead":1,"promote_whose":"any","exempt_tag":"snatcharr-no-gate"}}`, wantErr: false},
		{name: "positive watchaware gate seasons_on_add boundary 1 ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"seasons_on_add":1}}`, wantErr: false},
		{name: "positive watchaware gate seasons_on_add boundary 20 ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"seasons_on_add":20}}`, wantErr: false},
		{name: "negative watchaware gate seasons_on_add boundary 0 refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"seasons_on_add":0}}`, wantErr: true},
		{name: "negative watchaware gate seasons_on_add boundary 21 refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"seasons_on_add":21}}`, wantErr: true},
		{name: "negative watchaware gate mode act refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"mode":"act"}}`, wantErr: true},
		{name: "negative watchaware gate mode unknown refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"mode":"unknown"}}`, wantErr: true},
		{name: "negative watchaware gate unknown key refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"bogus":1}}`, wantErr: true},
		{name: "negative watchaware gate bad uuid refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"instance_ids":["not-a-uuid"]}}`, wantErr: true},
		{name: "positive watchaware gate valid uuid ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"instance_ids":["a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"]}}`, wantErr: false},
		{name: "positive watchaware gate look_ahead boundary 0 ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"look_ahead":0}}`, wantErr: false},
		{name: "positive watchaware gate look_ahead boundary 5 ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"look_ahead":5}}`, wantErr: false},
		{name: "negative watchaware gate look_ahead boundary 6 refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"look_ahead":6}}`, wantErr: true},
		{name: "positive watchaware gate promote_whose all ok", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"promote_whose":"all"}}`, wantErr: false},
		{name: "negative watchaware gate promote_whose invalid refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"promote_whose":"invalid"}}`, wantErr: true},
		{name: "negative watchaware gate exempt_tag empty refused", val: modules.ValidateWatchAwareConfig, raw: `{"gate":{"exempt_tag":""}}`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.val(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateConfig(%q) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("ValidateConfig(%q) err = %v, want ErrInvalid", tc.raw, err)
			}
		})
	}
}
