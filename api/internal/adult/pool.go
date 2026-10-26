// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/golusoris/goenvoy/metadata"
	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"

	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// MaxInstances bounds the number of stashbox instances processed concurrently or in one sweep (HISS-02, HISS-06).
	MaxInstances = 32
	// DefaultClientTimeout is the default per-request timeout for stashbox HTTP operations.
	DefaultClientTimeout = 30 * time.Second
)

// InstanceClient bundles a stash-box instance metadata with its initialized API client.
type InstanceClient struct {
	Instance sqlcgen.AdultStashboxInstance
	Client   *stashbox.Client
}

// ClientBuilder constructs authenticated stashbox clients from stored instances.
type ClientBuilder struct {
	enc     *crypto.Encryptor
	timeout time.Duration
	logger  *slog.Logger
}

// NewClientBuilder creates a ClientBuilder.
func NewClientBuilder(enc *crypto.Encryptor, timeout time.Duration, logger *slog.Logger) *ClientBuilder {
	if timeout <= 0 {
		timeout = DefaultClientTimeout
	}
	return &ClientBuilder{enc: enc, timeout: timeout, logger: logger}
}

// Build creates a stashbox client for a single instance by decrypting its secret.
// Plaintext API keys are never logged or returned.
func (b *ClientBuilder) Build(inst sqlcgen.AdultStashboxInstance) (*stashbox.Client, error) {
	keyBytes, err := b.enc.Open(inst.ApiKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("adult: decrypt api key for instance %q (%s): %w", inst.Name, inst.ID, err)
	}
	apiKey := string(keyBytes)
	client, err := stashbox.New(inst.Endpoint, apiKey, metadata.WithTimeout(b.timeout))
	if err != nil {
		return nil, fmt.Errorf("adult: init client for instance %q (%s): %w", inst.Name, inst.ID, err)
	}
	return client, nil
}

// BuildClients creates clients for a slice of enabled instances up to MaxInstances.
func (b *ClientBuilder) BuildClients(ctx context.Context, instances []sqlcgen.AdultStashboxInstance) []InstanceClient {
	if len(instances) > MaxInstances {
		b.logger.WarnContext(ctx, "adult: enabled instances exceed maximum limit; truncating",
			slog.Int("total", len(instances)),
			slog.Int("cap", MaxInstances),
		)
		instances = instances[:MaxInstances]
	}

	out := make([]InstanceClient, 0, len(instances))
	for _, inst := range instances {
		c, err := b.Build(inst)
		if err != nil {
			b.logger.WarnContext(ctx, "adult: failed to build client for instance",
				slog.String("instance_id", inst.ID.String()),
				slog.String("instance_name", inst.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		out = append(out, InstanceClient{Instance: inst, Client: c})
	}
	return out
}
