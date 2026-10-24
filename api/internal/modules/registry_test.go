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
