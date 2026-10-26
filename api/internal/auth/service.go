// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package auth owns local accounts, cookie sessions and API keys. Passwords are
// argon2id hashes (golusoris core/crypto); sessions and keys are stored in Postgres.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/golusoris/golusoris/auth/lockout"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	gerr "github.com/golusoris/golusoris/core/errors"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Lockout policy constants across all replicas (HISS-19 reuse golusoris auth/lockout).
const (
	LockoutMaxFails = 5
	LockoutWindow   = 15 * time.Minute
	LockoutCooldown = 15 * time.Minute
)

// SessionUserKey is the session data key holding the authenticated user's ID.
const SessionUserKey = "user_id"

// ExternalAccountSentinelHash is stored as password_hash for external SSO accounts.
// It is never a valid argon2id hash and auth.Login refuses it before VerifyPassword.
const ExternalAccountSentinelHash = "!sso-viewer-sentinel-hash!"

// ErrMemberDisabled indicates the Jellyfin user account is disabled.
var ErrMemberDisabled = errors.New("jellyfin member disabled")

// Service handles setup and login.
type Service struct {
	st        *store.Store
	hasher    *crypto.PasswordHasher
	clk       clock.Clock
	ids       id.Generator
	logger    *slog.Logger
	lockout   *lockout.Service
	dummyHash string

	testHookPostPasswordUpdate func(ctx context.Context)
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
	if err == nil && row.PasswordHash == ExternalAccountSentinelHash {
		s.recordFailures(ctx, userKey, ipKey)
		s.logger.InfoContext(ctx, "auth: login rejected: external account", slog.String("username", username), slog.String("ip", ip))
		return domain.User{}, fmt.Errorf("%w: invalid username or password", domain.ErrUnauthorized)
	}
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

// LoginJellyfin verifies Jellyfin credentials and enforces lockout counters on "jellyfin:"+lower(username).
func (s *Service) LoginJellyfin(ctx context.Context, a jellyfin.Authenticator, username, password, clientIP string) (jellyfin.Member, error) {
	trimmedUser := strings.TrimSpace(username)
	userKey := "jellyfin:" + strings.ToLower(trimmedUser)
	ip := strings.TrimSpace(clientIP)
	var ipKey string
	if ip != "" {
		ipKey = "ip:" + ip
	}
	if err := s.checkLockout(ctx, userKey, ipKey, username, ip); err != nil {
		return jellyfin.Member{}, err
	}
	m, err := a.SignIn(ctx, username, password)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			s.recordFailures(ctx, userKey, ipKey)
			s.logger.InfoContext(ctx, "auth: jellyfin login rejected", slog.String("username", username), slog.String("ip", ip))
			return jellyfin.Member{}, fmt.Errorf("%w: invalid username or password", domain.ErrUnauthorized)
		}
		return jellyfin.Member{}, fmt.Errorf("auth: jellyfin sign-in: %w", err)
	}
	if m.Disabled {
		s.recordFailures(ctx, userKey, ipKey)
		s.logger.InfoContext(ctx, "auth: jellyfin login rejected: user disabled", slog.String("username", username), slog.String("ip", ip))
		return jellyfin.Member{}, fmt.Errorf("%w: %w", domain.ErrUnauthorized, ErrMemberDisabled)
	}
	if err := s.lockout.Reset(ctx, userKey); err != nil {
		s.logger.WarnContext(ctx, "auth: reset lockout failed", slog.String("key", userKey), slog.String("error", err.Error()))
	}
	return m, nil
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

// FirstAdmin returns the first admin user ordered by created_at, id.
func (s *Service) FirstAdmin(ctx context.Context) (domain.User, error) {
	row, err := s.st.Q().GetFirstAdmin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: get first admin: %w", store.MapError(err))
	}
	return userFromRow(row), nil
}

// ExternalUsername derives the deterministic handle for an external SSO identity.
func ExternalUsername(prefix, identity string) string {
	h := sha256.Sum256([]byte(identity))
	return prefix + hex.EncodeToString(h[:])[:16]
}

// ProvisionExternalUser creates or loads an external account mapped to an SSO identity.
// Refuses admin and invalid roles with domain.ErrInvalid (admins map to FirstAdmin).
// Refuses user provisioning while setup is incomplete (SetupComplete false).
func (s *Service) ProvisionExternalUser(ctx context.Context, prefix, identity string, role domain.Role) (domain.User, error) {
	if !role.Valid() || role == domain.RoleAdmin {
		return domain.User{}, fmt.Errorf("%w: invalid external user role %q", domain.ErrInvalid, role)
	}
	setupDone, err := s.SetupComplete(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if !setupDone {
		return domain.User{}, fmt.Errorf("%w: setup incomplete", domain.ErrForbidden)
	}
	trimmed := strings.TrimSpace(identity)
	if trimmed == "" {
		return domain.User{}, fmt.Errorf("%w: empty external identity", domain.ErrInvalid)
	}
	username := ExternalUsername(prefix, trimmed)
	newID, err := s.ids.NewUUID()
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: new id: %w", err)
	}
	now := s.clk.Now()
	row, err := s.upsertExternalUser(ctx, sqlcgen.UpsertExternalUserParams{
		ID:           newID,
		Username:     username,
		PasswordHash: ExternalAccountSentinelHash,
		Role:         string(role),
		ExternalID:   &trimmed,
		CreatedAt:    now,
	})
	if err != nil {
		return domain.User{}, err
	}
	return userFromRow(row), nil
}

const maxExternalUpsertAttempts = 3

func (s *Service) upsertExternalUser(ctx context.Context, params sqlcgen.UpsertExternalUserParams) (sqlcgen.User, error) {
	var lastErr error
	for range maxExternalUpsertAttempts {
		row, err := s.st.Q().UpsertExternalUser(ctx, params)
		if err == nil {
			return row, nil
		}
		mapped := store.MapError(err)
		if !errors.Is(mapped, domain.ErrConflict) {
			return sqlcgen.User{}, fmt.Errorf("auth: upsert external user: %w", mapped)
		}
		lastErr = mapped
	}
	return sqlcgen.User{}, fmt.Errorf("auth: upsert external user: %w", lastErr)
}

// ProvisionJellyfinUser creates or loads an external account mapped to a Jellyfin user.
// Refuses admin and invalid roles with domain.ErrInvalid (admins map to FirstAdmin).
// Refuses user provisioning while setup is incomplete (SetupComplete false).
func (s *Service) ProvisionJellyfinUser(ctx context.Context, m jellyfin.Member, role domain.Role) (domain.User, error) {
	if !role.Valid() || role == domain.RoleAdmin {
		return domain.User{}, fmt.Errorf("%w: invalid external user role %q", domain.ErrInvalid, role)
	}
	setupDone, err := s.SetupComplete(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if !setupDone {
		return domain.User{}, fmt.Errorf("%w: setup incomplete", domain.ErrForbidden)
	}
	extID := "jellyfin:" + strings.TrimSpace(m.ID)
	if strings.TrimSpace(m.ID) == "" {
		return domain.User{}, fmt.Errorf("%w: empty jellyfin user id", domain.ErrInvalid)
	}

	candidates := jellyfinHandleCandidates(m)
	newID, err := s.ids.NewUUID()
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: new id: %w", err)
	}
	now := s.clk.Now()

	for _, username := range candidates {
		row, err := s.st.Q().UpsertExternalUser(ctx, sqlcgen.UpsertExternalUserParams{
			ID:           newID,
			Username:     username,
			PasswordHash: ExternalAccountSentinelHash,
			Role:         string(role),
			ExternalID:   &extID,
			CreatedAt:    now,
		})
		if err == nil {
			return userFromRow(row), nil
		}
		if isUsernameConflict(err) {
			continue
		}
		return domain.User{}, fmt.Errorf("auth: upsert jellyfin user: %w", store.MapError(err))
	}
	return domain.User{}, fmt.Errorf("%w: could not allocate unique username for jellyfin user", domain.ErrConflict)
}

func jellyfinHandleCandidates(m jellyfin.Member) []string {
	base := "jf-" + strings.ToLower(strings.TrimSpace(m.Name))
	if domain.ValidateUsername(base) != nil {
		return []string{ExternalUsername("jf-", m.ID)}
	}
	candidates := make([]string, 0, 10)
	candidates = append(candidates, base)
	for i := 2; i <= 9; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if domain.ValidateUsername(cand) == nil {
			candidates = append(candidates, cand)
		}
	}
	candidates = append(candidates, ExternalUsername("jf-", m.ID))
	return candidates
}

func isUsernameConflict(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "username")
	}
	return errors.Is(err, domain.ErrConflict) && strings.Contains(err.Error(), "username")
}

// ChangePassword verifies current password, updates it to newPassword, and ends every session of
// the user, the calling one included; the caller signs the user in again with RenewSession (#268).
// API keys are left untouched. Wrong current password counts against user lockout.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	row, err := s.st.Q().GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: get user: %w", store.MapError(err))
	}
	userKey, err := s.verifyCurrentPassword(ctx, row.Username, currentPassword, row.PasswordHash)
	if err != nil {
		return err
	}
	hash, err := s.hashNewPassword(ctx, newPassword)
	if err != nil {
		return err
	}
	if err := s.commitPasswordChange(ctx, userID, hash); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if rstErr := s.lockout.Reset(ctx, userKey); rstErr != nil {
		s.logger.WarnContext(ctx, "auth: reset lockout failed", slog.String("key", userKey), slog.String("error", rstErr.Error()))
	}
	return nil
}

func (s *Service) verifyCurrentPassword(ctx context.Context, username, currentPassword, passwordHash string) (string, error) {
	userKey := "user:" + strings.ToLower(username)
	if checkErr := s.checkKey(ctx, userKey); checkErr != nil {
		return "", checkErr
	}
	match, _, verr := crypto.VerifyPassword(currentPassword, passwordHash)
	if verr != nil {
		return "", fmt.Errorf("auth: verify password: %w", verr)
	}
	if !match {
		s.recordFailures(ctx, userKey, "")
		s.logger.InfoContext(ctx, "auth: password change rejected", slog.String("username", username))
		return "", fmt.Errorf("%w: invalid current password", domain.ErrUnauthorized)
	}
	return userKey, nil
}

func (s *Service) hashNewPassword(ctx context.Context, newPassword string) (string, error) {
	if valErr := domain.ValidatePassword(newPassword); valErr != nil {
		return "", fmt.Errorf("auth: %w", valErr)
	}
	hash, hashErr := s.hasher.Hash(ctx, newPassword)
	if hashErr != nil {
		return "", fmt.Errorf("auth: hash password: %w", hashErr)
	}
	return hash, nil
}

func (s *Service) commitPasswordChange(ctx context.Context, userID uuid.UUID, hash string) error {
	now := s.clk.Now()
	return s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		if updErr := q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{
			ID: userID, PasswordHash: hash, UpdatedAt: now,
		}); updErr != nil {
			return fmt.Errorf("update password: %w", store.MapError(updErr))
		}
		if s.testHookPostPasswordUpdate != nil {
			s.testHookPostPasswordUpdate(ctx)
		}
		// No session id is empty, so every session of the user goes.
		if _, delErr := q.DeleteUserSessionsExcept(ctx, sqlcgen.DeleteUserSessionsExceptParams{
			UserID: userID.String(), CurrentSessionID: "",
		}); delErr != nil {
			return fmt.Errorf("delete sessions: %w", store.MapError(delErr))
		}
		return nil
	})
}

func userFromRow(r sqlcgen.User) domain.User {
	return domain.User{ID: r.ID, Username: r.Username, Role: domain.Role(r.Role), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
