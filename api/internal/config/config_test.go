// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	prod := config.Default()
	prod.Worker.Token = "t"
	prod.APIKey.Secret = "s"
	tests := []struct {
		name    string
		mutate  func(*config.Options)
		wantErr error
		wantAny bool
	}{
		{"positive prod with secrets", func(*config.Options) {}, nil, false},
		{"positive dev without secrets", func(o *config.Options) {
			o.Profile = config.ProfileDev
			o.Worker.Token = ""
			o.APIKey.Secret = ""
		}, nil, false},
		{"negative prod missing worker token", func(o *config.Options) { o.Worker.Token = "" }, config.ErrMissingSecret, true},
		{"negative prod missing apikey secret", func(o *config.Options) { o.APIKey.Secret = "" }, config.ErrMissingSecret, true},
		{"negative unknown profile", func(o *config.Options) { o.Profile = "staging" }, nil, true},
		{"boundary ttl 1m ok", func(o *config.Options) { o.Session.TTL = time.Minute }, nil, false},
		{"boundary ttl 59s", func(o *config.Options) { o.Session.TTL = 59 * time.Second }, nil, true},
		{"boundary ttl 720h ok", func(o *config.Options) { o.Session.TTL = 720 * time.Hour }, nil, false},
		{"boundary ttl 721h", func(o *config.Options) { o.Session.TTL = 721 * time.Hour }, nil, true},
		{"negative secrets without config", func(o *config.Options) { o.Configarr.Secrets = "/s.yml" }, nil, true},
		{"positive configarr linked", func(o *config.Options) {
			o.Configarr.Config = "/c.yml"
			o.Configarr.Secrets = "/s.yml"
		}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := prod
			tc.mutate(&o)
			err := o.Validate()
			if (err != nil) != tc.wantAny {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantAny)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLinked(t *testing.T) {
	t.Parallel()
	if (config.ConfigarrOptions{}).Linked() {
		t.Fatal("empty options must not be linked")
	}
	if !(config.ConfigarrOptions{Config: "/c.yml"}).Linked() {
		t.Fatal("config path must mean linked")
	}
}
