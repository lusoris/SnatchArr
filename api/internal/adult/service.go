// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// ModuleChecker provides module status and configuration lookup.
type ModuleChecker interface {
	IsEnabled(ctx context.Context, id string) (bool, error)
	Get(ctx context.Context, id string) (domain.Module, error)
}

// Service provides Stash-box synchronization, pooling, and federated search.
type Service struct {
	st       *store.Store
	enc      *crypto.Encryptor
	clk      clock.Clock
	ids      id.Generator
	mod      ModuleChecker
	pool     *ClientBuilder
	logger   *slog.Logger
	mu       sync.Mutex
	lastPass time.Time
}

// NewService constructs an adult Service.
func NewService(
	st *store.Store,
	enc *crypto.Encryptor,
	clk clock.Clock,
	ids id.Generator,
	mod *modules.Service,
	logger *slog.Logger,
) *Service {
	pool := NewClientBuilder(enc, DefaultClientTimeout, logger)
	return &Service{
		st:     st,
		enc:    enc,
		clk:    clk,
		ids:    ids,
		mod:    mod,
		pool:   pool,
		logger: logger,
	}
}

// NewServiceWithChecker allows injecting a custom ModuleChecker for testing.
func NewServiceWithChecker(
	st *store.Store,
	enc *crypto.Encryptor,
	clk clock.Clock,
	ids id.Generator,
	mod ModuleChecker,
	logger *slog.Logger,
) *Service {
	pool := NewClientBuilder(enc, DefaultClientTimeout, logger)
	return &Service{
		st:     st,
		enc:    enc,
		clk:    clk,
		ids:    ids,
		mod:    mod,
		pool:   pool,
		logger: logger,
	}
}

// ClientForTest creates a stashbox client for test inspection.
func (s *Service) ClientForTest(inst sqlcgen.AdultStashboxInstance) (*stashbox.Client, error) {
	return s.pool.Build(inst)
}
