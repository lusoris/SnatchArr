// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"encoding/hex"
	"fmt"
	"log/slog"

	gconfig "github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

// newEncryptor builds the encryptor for *arr API keys and download-client secrets from the
// key the profile may run on (config.Options.CryptoKey). It replaces golusoris' provider,
// which refuses an empty crypto.key in every profile: as an fx decorator that does not take
// the original value, it keeps that provider from running at all (#138).
//
// The dev profile without a key runs on the published development key and says so on every
// start; prod never reaches this with an empty or development key, because config.Load
// refuses both.
func newEncryptor(cfg *gconfig.Config, o config.Options, logger *slog.Logger) (*crypto.Encryptor, error) {
	hexKey, development, err := o.CryptoKey(cfg.String("crypto.key"))
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	if development {
		logger.Warn("crypto: running on the published development key; *arr API keys and download-client " +
			"secrets stored now are NOT protected. Set APP_CRYPTO_KEY (openssl rand -hex 32) before storing real credentials.")
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("app: decode crypto.key: %w", err)
	}
	enc, err := crypto.NewEncryptor(key)
	if err != nil {
		return nil, fmt.Errorf("app: crypto.key: %w", err)
	}
	return enc, nil
}
