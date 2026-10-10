// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config_test

import (
	"encoding/hex"
	"errors"
	"strings"
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

// A key of the deployment's own, as `openssl rand -hex 32` prints it.
const ownKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestCryptoKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		profile    config.Profile
		configured string
		wantKey    string
		wantDev    bool
		wantErr    error
	}{
		{"positive prod with its own key", config.ProfileProd, ownKey, ownKey, false, nil},
		{"positive dev without a key runs on the development key", config.ProfileDev, "", config.DevCryptoKey, true, nil},
		{"positive dev with its own key", config.ProfileDev, ownKey, ownKey, false, nil},
		{"boundary dev set to the development key still reports development", config.ProfileDev, config.DevCryptoKey, config.DevCryptoKey, true, nil},
		{"negative prod without a key", config.ProfileProd, "", "", false, config.ErrMissingSecret},
		{"negative prod with the development key", config.ProfileProd, config.DevCryptoKey, "", false, config.ErrDevelopmentKey},
		{"negative prod with the development key in upper case", config.ProfileProd, strings.ToUpper(config.DevCryptoKey), "", false, config.ErrDevelopmentKey},
		{"negative default profile without a key", config.Default().Profile, "", "", false, config.ErrMissingSecret},
		{"negative default profile with the development key", config.Default().Profile, config.DevCryptoKey, "", false, config.ErrDevelopmentKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := config.Default()
			o.Profile = tc.profile
			key, dev, err := o.CryptoKey(tc.configured)
			if !errors.Is(err, tc.wantErr) || key != tc.wantKey || dev != tc.wantDev {
				t.Fatalf("CryptoKey(%q) = %q, %v, %v; want %q, %v, %v", tc.configured, key, dev, err, tc.wantKey, tc.wantDev, tc.wantErr)
			}
		})
	}
}

// The development key is a valid AES-256 key: 64 hex digits.
func TestDevCryptoKeyIsAES256(t *testing.T) {
	t.Parallel()
	raw, err := hex.DecodeString(config.DevCryptoKey)
	if err != nil || len(raw) != 32 {
		t.Fatalf("DevCryptoKey decodes to %d bytes, %v; want 32", len(raw), err)
	}
}
