// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package requests

import (
	"context"
	"time"

	"go.uber.org/fx"
)

const defaultIOTimeout = 5 * time.Second

// Service coordinates requests settings operations with deadlines.
type Service struct {
	store *Store
}

// NewService constructs a Service.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Get returns current settings with an explicit I/O deadline.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultIOTimeout)
		defer cancel()
	}
	return s.store.Get(ctx)
}

// Put persists new settings with an explicit I/O deadline.
func (s *Service) Put(ctx context.Context, in Settings) (Settings, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultIOTimeout)
		defer cancel()
	}
	return s.store.Put(ctx, in)
}

// Module provides the requests Store and Service to fx.
var Module = fx.Module(
	"snatcharr.requests",
	fx.Provide(NewStore, NewService),
)
