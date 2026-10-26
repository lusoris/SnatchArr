// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func testEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("crypto.NewEncryptor error: %v", err)
	}
	return enc
}

func TestClientBuilder_Build(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	logger := slog.New(slog.DiscardHandler)
	builder := NewClientBuilder(enc, 5*time.Second, logger)

	apiKey := "a-key-for-the-stub"
	sealed, err := enc.Seal([]byte(apiKey))
	if err != nil {
		t.Fatalf("Seal error: %v", err)
	}

	validInst := sqlcgen.AdultStashboxInstance{
		ID:        uuid.New(),
		Name:      "StashDB",
		Endpoint:  "https://stashdb.org/graphql",
		ApiKeyEnc: sealed,
		Enabled:   true,
	}

	client, err := builder.Build(validInst)
	if err != nil {
		t.Fatalf("builder.Build unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Corrupt key
	corruptInst := validInst
	corruptInst.ApiKeyEnc = []byte("corrupted-ciphertext")
	_, err = builder.Build(corruptInst)
	if err == nil {
		t.Fatal("expected error with corrupted ciphertext, got nil")
	}
}

func TestClientBuilder_BuildClients_Limit(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	logger := slog.New(slog.DiscardHandler)
	builder := NewClientBuilder(enc, 5*time.Second, logger)

	sealed, _ := enc.Seal([]byte("api-key"))
	instances := make([]sqlcgen.AdultStashboxInstance, 0, MaxInstances+10)
	for range MaxInstances + 10 {
		instances = append(instances, sqlcgen.AdultStashboxInstance{
			ID:        uuid.New(),
			Name:      "StashInstance",
			Endpoint:  "https://stashdb.org/graphql",
			ApiKeyEnc: sealed,
			Enabled:   true,
		})
	}

	ctx := context.Background()
	built := builder.BuildClients(ctx, instances)
	if len(built) != MaxInstances {
		t.Fatalf("expected built clients capped at %d, got %d", MaxInstances, len(built))
	}
}
