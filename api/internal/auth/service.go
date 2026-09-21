// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package auth owns local accounts, cookie sessions and API keys. Passwords are
// argon2id hashes (golusoris core/crypto); sessions and keys are stored in Postgres.
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Service handles setup and login.
type Service struct {
	st        *store.Store
	hasher    *crypto.PasswordHasher
	clk       clock.Clock
	ids       id.Generator
	logger    *slog.Logger
	dummyHash string
}

// NewService wires the service. A dummy hash is precomputed so a login against an
// unknown username costs the same as a wrong password (no user enumeration by timing).
func NewService(st *store.Store, hasher *crypto.PasswordHasher, clk clock.Clock, ids id.Generator, logger *slog.Logger) (*Service, error) {
	dummy, err := crypto.HashPassword("snatcharr-dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	return &Service{st: st, hasher: hasher, clk: clk, ids: ids, logger: logger, dummyHash: dummy}, nil
}

// SetupComplete reports whether at least one user exists.
func (s *Service) SetupComplete(ctx context.Context) (bool, error) {
	n, err := s.st.Q().CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: count users: %w", store.MapError(err))
	}
	return n > 0, nil
}

// Setup creates the first admin. It refuses once any user exists.
func (s *Service) Setup(ctx context.Context, username, password string) (domain.User, error) {
	done, err := s.SetupComplete(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if done {
		return domain.User{}, fmt.Errorf("%w: setup already completed", domain.ErrConflict)
	}
	return s.createUser(ctx, username, password, domain.RoleAdmin)
}

func (s *Service) createUser(ctx context.Context, username, password string, role domain.Role) (domain.User, error) {
	username = strings.TrimSpace(username)
	if err := domain.ValidateUsername(username); err != nil {
		return domain.User{}, fmt.Errorf("auth: %w", err)
	}
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, fmt.Errorf("auth: %w", err)
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: hash password: %w", err)
	}
	userID, err := s.ids.NewUUID()
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: new id: %w", err)
	}
	row, err := s.st.Q().CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: userID, Username: username, PasswordHash: hash, Role: string(role), CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: create user: %w", store.MapError(err))
	}
	return userFromRow(row), nil
}

// Login verifies credentials. Any failure is ErrUnauthorized; the reason is logged only.
func (s *Service) Login(ctx context.Context, username, password string) (domain.User, error) {
	row, err := s.st.Q().GetUserByUsername(ctx, strings.TrimSpace(username))
	hash := s.dummyHash
	if err == nil {
		hash = row.PasswordHash
	}
	match, _, verr := crypto.VerifyPassword(password, hash)
	if verr != nil {
		return domain.User{}, fmt.Errorf("auth: verify password: %w", verr)
	}
	if err != nil || !match {
		s.logger.InfoContext(ctx, "auth: login rejected", slog.String("username", username))
		return domain.User{}, fmt.Errorf("%w: invalid username or password", domain.ErrUnauthorized)
	}
	return userFromRow(row), nil
}

// User loads an account by id.
func (s *Service) User(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	row, err := s.st.Q().GetUser(ctx, userID)
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: get user: %w", store.MapError(err))
	}
	return userFromRow(row), nil
}

func userFromRow(r sqlcgen.User) domain.User {
	return domain.User{ID: r.ID, Username: r.Username, Role: domain.Role(r.Role), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
