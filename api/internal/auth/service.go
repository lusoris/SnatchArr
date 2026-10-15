// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package auth owns local accounts, cookie sessions and API keys. Passwords are
// argon2id hashes (golusoris core/crypto); sessions and keys are stored in Postgres.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/lockout"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	gerr "github.com/golusoris/golusoris/core/errors"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Lockout policy constants across all replicas (HISS-19 reuse golusoris auth/lockout).
const (
	LockoutMaxFails = 5
	LockoutWindow   = 15 * time.Minute
	LockoutCooldown = 15 * time.Minute
)

// Service handles setup and login.
type Service struct {
	st        *store.Store
	hasher    *crypto.PasswordHasher
	clk       clock.Clock
	ids       id.Generator
	logger    *slog.Logger
	lockout   *lockout.Service
	dummyHash string
}

// NewService wires the service. A dummy hash is precomputed so a login against an
// unknown username costs the same as a wrong password (no user enumeration by timing).
func NewService(st *store.Store, hasher *crypto.PasswordHasher, clk clock.Clock, ids id.Generator, logger *slog.Logger) (*Service, error) {
	dummy, err := crypto.HashPassword("snatcharr-dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	lo, err := lockout.New(NewPgLockoutStore(st), clk, lockout.Options{
		MaxFails: LockoutMaxFails,
		Window:   LockoutWindow,
		Cooldown: LockoutCooldown,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: lockout service: %w", err)
	}
	return &Service{
		st:        st,
		hasher:    hasher,
		clk:       clk,
		ids:       ids,
		logger:    logger,
		lockout:   lo,
		dummyHash: dummy,
	}, nil
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

// Login verifies credentials. Any failure is ErrUnauthorized or ErrTooManyRequests.
func (s *Service) Login(ctx context.Context, username, password, clientIP string) (domain.User, error) {
	trimmedUser := strings.TrimSpace(username)
	userKey := "user:" + strings.ToLower(trimmedUser)
	ip := strings.TrimSpace(clientIP)
	var ipKey string
	if ip != "" {
		ipKey = "ip:" + ip
	}
	if err := s.checkLockout(ctx, userKey, ipKey, username, ip); err != nil {
		return domain.User{}, err
	}
	row, err := s.st.Q().GetUserByUsername(ctx, trimmedUser)
	hash := s.dummyHash
	if err == nil {
		hash = row.PasswordHash
	}
	match, _, verr := crypto.VerifyPassword(password, hash)
	if verr != nil {
		return domain.User{}, fmt.Errorf("auth: verify password: %w", verr)
	}
	if err != nil || !match {
		s.recordFailures(ctx, userKey, ipKey)
		s.logger.InfoContext(ctx, "auth: login rejected", slog.String("username", username), slog.String("ip", ip))
		return domain.User{}, fmt.Errorf("%w: invalid username or password", domain.ErrUnauthorized)
	}
	if err := s.lockout.Reset(ctx, userKey); err != nil {
		s.logger.WarnContext(ctx, "auth: reset lockout failed", slog.String("key", userKey), slog.String("error", err.Error()))
	}
	return userFromRow(row), nil
}

func (s *Service) checkLockout(ctx context.Context, userKey, ipKey, username, ip string) error {
	if err := s.checkKey(ctx, userKey); err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			s.logger.WarnContext(ctx, "auth: login locked", slog.String("username", username), slog.String("ip", ip))
		}
		return err
	}
	if ipKey != "" {
		if err := s.checkKey(ctx, ipKey); err != nil {
			if errors.Is(err, domain.ErrTooManyRequests) {
				s.logger.WarnContext(ctx, "auth: login locked", slog.String("username", username), slog.String("ip", ip))
			}
			return err
		}
	}
	return nil
}

func (s *Service) checkKey(ctx context.Context, key string) error {
	if err := s.lockout.Check(ctx, key); err != nil {
		if isLocked(err) {
			return fmt.Errorf("%w: account locked", domain.ErrTooManyRequests)
		}
		return fmt.Errorf("auth: check lockout: %w", err)
	}
	return nil
}

func (s *Service) recordFailures(ctx context.Context, userKey, ipKey string) {
	if err := s.lockout.Fail(ctx, userKey); err != nil {
		s.logger.WarnContext(ctx, "auth: record failure failed", slog.String("key", userKey), slog.String("error", err.Error()))
	}
	if ipKey != "" {
		if err := s.lockout.Fail(ctx, ipKey); err != nil {
			s.logger.WarnContext(ctx, "auth: record failure failed", slog.String("key", ipKey), slog.String("error", err.Error()))
		}
	}
}

func isLocked(err error) bool {
	var ge *gerr.Error
	return errors.As(err, &ge) && ge.Code == gerr.CodeUnauthorized
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
