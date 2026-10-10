// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"bytes"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

// A key of the deployment's own, as `openssl rand -hex 32` prints it.
const ownKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// cryptoGraph builds the part of the fx graph that yields the encryptor, as the app wires it:
// golusoris' crypto module, the snatcharr config and (unless bare) the newEncryptor
// decorator. It returns the encryptor, what was logged and the graph error.
func cryptoGraph(t *testing.T, configJSON string, bare bool) (*crypto.Encryptor, string, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// The prefix matches no variable, so the test reads the file only.
	cfg, err := gconfig.New(gconfig.Options{Files: []string{file}, EnvPrefix: "SNATCHARR_CRYPTO_TEST_UNSET_"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	var logs bytes.Buffer
	var enc *crypto.Encryptor
	opts := []fx.Option{
		fx.NopLogger, fx.Supply(cfg, slog.New(slog.NewTextHandler(&logs, nil))),
		fx.Provide(config.Load), crypto.Module, fx.Populate(&enc),
	}
	if !bare {
		opts = append(opts, fx.Decorate(newEncryptor))
	}
	// Build the graph before reading enc and the log: operands of a return are evaluated in order.
	graphErr := fx.New(opts...).Err()
	return enc, logs.String(), graphErr
}

// The dev profile starts without APP_CRYPTO_KEY, runs on the published development key and
// says that what it stores is unprotected (#138).
func TestDevProfileStartsOnTheDevelopmentKey(t *testing.T) {
	t.Parallel()
	enc, logs, err := cryptoGraph(t, `{"snatcharr":{"profile":"dev"}}`, false)
	if err != nil || enc == nil {
		t.Fatalf("dev without a key: encryptor %v, %v", enc, err)
	}
	sealed, err := enc.Seal([]byte("arr-api-key"))
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	devKey, err := hex.DecodeString(config.DevCryptoKey)
	if err != nil {
		t.Fatal(err)
	}
	if plain, openErr := crypto.Open(devKey, sealed); openErr != nil || string(plain) != "arr-api-key" {
		t.Fatalf("the published development key does not open what dev sealed: %q, %v", plain, openErr)
	}
	if !strings.Contains(logs, "development key") || !strings.Contains(logs, "NOT protected") {
		t.Fatalf("no warning that the development key leaves secrets unprotected; logged: %q", logs)
	}
}

// Without the decorator golusoris refuses the same configuration: that is the bug.
func TestGolusorisAloneRefusesDevWithoutAKey(t *testing.T) {
	t.Parallel()
	_, _, err := cryptoGraph(t, `{"snatcharr":{"profile":"dev"}}`, true)
	if err == nil || !strings.Contains(err.Error(), "crypto.key is required") {
		t.Fatalf("golusoris crypto module alone = %v, want its crypto.key refusal", err)
	}
}

// Prod, which is also the default profile, starts only on a key of its own.
func TestProdRefusesMissingAndDevelopmentKeys(t *testing.T) {
	t.Parallel()
	prodSecrets := `"worker":{"token":"t"},"apikey":{"secret":"s"}`
	tests := []struct {
		name    string
		config  string
		wantErr error
	}{
		{"negative prod without a key", `{"snatcharr":{"profile":"prod",` + prodSecrets + `}}`, config.ErrMissingSecret},
		{"negative prod with the development key", `{"snatcharr":{"profile":"prod",` + prodSecrets + `},"crypto":{"key":"` + config.DevCryptoKey + `"}}`, config.ErrDevelopmentKey},
		{"negative default profile without a key", `{"snatcharr":{` + prodSecrets + `}}`, config.ErrMissingSecret},
		{"negative default profile with the development key", `{"snatcharr":{` + prodSecrets + `},"crypto":{"key":"` + config.DevCryptoKey + `"}}`, config.ErrDevelopmentKey},
		{"positive prod with its own key", `{"snatcharr":{"profile":"prod",` + prodSecrets + `},"crypto":{"key":"` + ownKey + `"}}`, nil},
		{"positive dev with its own key", `{"snatcharr":{"profile":"dev"},"crypto":{"key":"` + ownKey + `"}}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enc, logs, err := cryptoGraph(t, tc.config, false)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && (err != nil || enc == nil)) {
				t.Fatalf("graph error = %v, encryptor %v; want %v", err, enc, tc.wantErr)
			}
			if strings.Contains(logs, "development key") {
				t.Fatalf("a deployment on its own key, or one that refused to start, warned about the development key: %q", logs)
			}
		})
	}
}
