// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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

const (
	// MaxThumbnailConcurrency is the scalar upper bound on concurrent thumbnail generation workers.
	MaxThumbnailConcurrency = 2
)

// Service provides Stash-box synchronization, pooling, federated search, and image set management.
type Service struct {
	st           *store.Store
	enc          *crypto.Encryptor
	clk          clock.Clock
	ids          id.Generator
	mod          ModuleChecker
	pool         *ClientBuilder
	logger       *slog.Logger
	mu           sync.Mutex
	lastPass     time.Time
	lastScanPass time.Time
	thumbSem     chan struct{}
	openFile     func(root *os.Root, rel string) (*os.File, error)
	maxImageSets int
}

func defaultRootOpen(root *os.Root, rel string) (*os.File, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("adult: root open: %w", err)
	}
	return f, nil
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
		st:           st,
		enc:          enc,
		clk:          clk,
		ids:          ids,
		mod:          mod,
		pool:         pool,
		logger:       logger,
		thumbSem:     make(chan struct{}, MaxThumbnailConcurrency),
		openFile:     defaultRootOpen,
		maxImageSets: 0,
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
		st:           st,
		enc:          enc,
		clk:          clk,
		ids:          ids,
		mod:          mod,
		pool:         pool,
		logger:       logger,
		thumbSem:     make(chan struct{}, MaxThumbnailConcurrency),
		openFile:     defaultRootOpen,
		maxImageSets: 0,
	}
}

// ClientForTest creates a stashbox client for test inspection.
func (s *Service) ClientForTest(inst sqlcgen.AdultStashboxInstance) (*stashbox.Client, error) {
	return s.pool.Build(inst)
}
