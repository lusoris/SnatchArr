// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestUserTargetStore_RefusesUnsealedAndHidesPlaintext(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("crypto.NewEncryptor: %v", err)
	}
	uts := NewUserTargetStore(st, enc, clk)

	userID := uuid.New()
	_, err = st.Pool().Exec(t.Context(),
		"INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, 'store-user', 'hash', 'viewer', NOW(), NOW())",
		userID,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	const plantedSecret = "super-secret-planted-webhook-token-99999"

	// 1. Negative / Boundary: store write handed unsealed bytes is refused on Insert.
	unsealedBytes := []byte("plaintext-unsealed-config-" + plantedSecret)
	_, err = uts.InsertTarget(t.Context(), uuid.New(), userID, "discord", unsealedBytes, true)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("InsertTarget(unsealed) err = %v, want domain.ErrInvalid", err)
	}

	// 2. Negative / Boundary: store write handed unsealed bytes is refused on Update.
	_, err = uts.UpdateTarget(t.Context(), uuid.New(), userID, "discord", unsealedBytes, true)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("UpdateTarget(unsealed) err = %v, want domain.ErrInvalid", err)
	}

	// 3. Positive: sealed insert.
	cfg := domain.ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/123/" + plantedSecret}
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	sealedCfg, err := enc.Seal(rawCfg)
	if err != nil {
		t.Fatalf("enc.Seal: %v", err)
	}

	targetID := uuid.New()
	row, err := uts.InsertTarget(t.Context(), targetID, userID, "discord", sealedCfg, true)
	if err != nil {
		t.Fatalf("InsertTarget(sealed): %v", err)
	}
	if row.ID != targetID {
		t.Fatalf("row.ID = %s, want %s", row.ID, targetID)
	}

	// 4. Security check: after create, the raw bytea column does not contain the plaintext secret.
	var rawDBBytes []byte
	err = st.Pool().QueryRow(t.Context(),
		"SELECT config_enc FROM user_notify_targets WHERE id = $1", targetID,
	).Scan(&rawDBBytes)
	if err != nil {
		t.Fatalf("query raw config_enc: %v", err)
	}
	if bytes.Contains(rawDBBytes, []byte(plantedSecret)) {
		t.Fatalf("security violation: raw config_enc contains plaintext secret bytes")
	}

	// 5. Positive: GetTarget, ListTargets, DeleteTarget.
	got, err := uts.GetTarget(t.Context(), targetID, userID)
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if got.ID != targetID || got.UserID != userID {
		t.Fatalf("GetTarget returned unexpected row: %+v", got)
	}

	list, err := uts.ListTargets(t.Context(), userID)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(list) != 1 || list[0].ID != targetID {
		t.Fatalf("ListTargets returned %+v, want 1 target %s", list, targetID)
	}

	if delErr := uts.DeleteTarget(t.Context(), targetID, userID); delErr != nil {
		t.Fatalf("DeleteTarget: %v", delErr)
	}

	_, err = uts.GetTarget(t.Context(), targetID, userID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetTarget after delete err = %v, want domain.ErrNotFound", err)
	}
}
