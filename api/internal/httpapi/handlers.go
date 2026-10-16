// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/csrf"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/cleanuparr"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Handlers implements oas.Handler.
type Handlers struct {
	build      buildinfo.Info
	cfg        config.Options
	clk        clock.Clock
	auth       *auth.Service
	apiKeys    *auth.APIKeys
	sessions   *session.Manager
	instances  *instances.Service
	policies   *policies.Service
	runs       *snatch.Runs
	workers    *snatch.Workers
	rec        *snatch.Recorder
	budget     *snatch.Budget
	memory     *snatch.Memory
	planner    *snatch.Planner
	schedules  *snatch.Schedules
	clients    *dlclients.Service
	settings   *settings.Service
	seerr      *seerr.Service
	cleanuparr *cleanuparr.Service
	configarr  *configarr.Service
	logger     *slog.Logger
}

// Deps groups the snatch services the handlers need.
type Deps struct {
	fx.In
	Runs       *snatch.Runs
	Workers    *snatch.Workers
	Rec        *snatch.Recorder
	Budget     *snatch.Budget
	Memory     *snatch.Memory
	Planner    *snatch.Planner
	Schedules  *snatch.Schedules
	Clients    *dlclients.Service
	Settings   *settings.Service
	Seerr      *seerr.Service
	Cleanuparr *cleanuparr.Service
	Configarr  *configarr.Service
}

// NewHandlers wires the operation handlers.
func NewHandlers(build buildinfo.Info, cfg config.Options, clk clock.Clock, authSvc *auth.Service,
	apiKeys *auth.APIKeys, sessions *session.Manager, inst *instances.Service, pol *policies.Service, logger *slog.Logger, d Deps,
) *Handlers {
	return &Handlers{
		build: build, cfg: cfg, clk: clk, auth: authSvc, apiKeys: apiKeys, sessions: sessions, instances: inst, policies: pol,
		runs: d.Runs, workers: d.Workers, rec: d.Rec, budget: d.Budget, memory: d.Memory, planner: d.Planner, schedules: d.Schedules, clients: d.Clients, settings: d.Settings, seerr: d.Seerr, cleanuparr: d.Cleanuparr, configarr: d.Configarr, logger: logger,
	}
}

var _ oas.Handler = (*Handlers)(nil)

// NewError renders handler errors as RFC 9457 (ogen "convenient error" hook).
func (h *Handlers) NewError(_ context.Context, err error) *oas.ProblemStatusCode {
	status, detail, params := statusOf(err)
	items := make([]oas.ProblemInvalidMinusParamsItem, 0, len(params))
	for _, p := range params {
		items = append(items, oas.ProblemInvalidMinusParamsItem{Name: p.Name, Reason: p.Reason})
	}
	return &oas.ProblemStatusCode{
		StatusCode: status,
		Response: oas.Problem{
			Type:               url.URL{Scheme: "about", Opaque: "blank"},
			Title:              httpStatusText(status),
			Status:             status,
			Detail:             optString(detail),
			InvalidMinusParams: items,
		},
	}
}

// GetSystemStatus is public so the SPA can render a setup or login screen.
func (h *Handlers) GetSystemStatus(ctx context.Context) (*oas.SystemStatus, error) {
	done, err := h.auth.SetupComplete(ctx)
	if err != nil {
		return nil, err
	}
	contact, seen, err := h.workers.LastContact(ctx)
	if err != nil {
		return nil, err
	}
	out := &oas.SystemStatus{
		Name: buildinfo.Name, Version: h.build.Version, Commit: h.build.Commit, BuildDate: h.build.Date,
		Profile: oas.SystemStatusProfile(h.cfg.Profile), SetupComplete: done,
	}
	if seen {
		out.WorkerLastContactAt = oas.NewOptDateTime(contact)
	}
	return out, nil
}

// GetSetupStatus reports whether the first user exists and whether a setup secret is required.
func (h *Handlers) GetSetupStatus(ctx context.Context) (*oas.SetupStatus, error) {
	done, err := h.auth.SetupComplete(ctx)
	if err != nil {
		return nil, err
	}
	return &oas.SetupStatus{
		SetupComplete:  done,
		SecretRequired: h.cfg.Setup.Secret != "",
	}, nil
}

// CompleteSetup creates the first admin and logs them in.
func (h *Handlers) CompleteSetup(ctx context.Context, req *oas.SetupRequest) (oas.CompleteSetupRes, error) {
	done, err := h.auth.SetupComplete(ctx)
	if err != nil {
		return nil, err
	}
	if done {
		return nil, fmt.Errorf("%w: setup already completed", domain.ErrConflict)
	}
	if h.cfg.Setup.Secret != "" {
		secret, ok := req.SetupSecret.Get()
		if !ok || subtle.ConstantTimeCompare([]byte(secret), []byte(h.cfg.Setup.Secret)) != 1 {
			return nil, fmt.Errorf("%w: invalid or missing setup secret", domain.ErrUnauthorized)
		}
	}
	u, err := h.auth.Setup(ctx, req.Username, req.Password)
	if err != nil {
		return nil, err
	}
	return h.startSession(ctx, u)
}

// ChangePassword changes the current user's password and ends all other sessions.
func (h *Handlers) ChangePassword(ctx context.Context, req *oas.ChangePasswordRequest) (oas.ChangePasswordRes, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	r, ok := requestFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: no http context", domain.ErrUnauthorized)
	}
	sess, err := h.sessions.Load(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	if err := h.auth.ChangePassword(ctx, u.ID, req.CurrentPassword, req.NewPassword, sess.ID); err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			if w, okW := writerFrom(ctx); okW {
				w.Header().Set("Retry-After", strconv.Itoa(int(auth.LockoutCooldown.Seconds())))
			}
		}
		return nil, err
	}
	return &oas.ChangePasswordNoContent{}, nil
}

// Login verifies credentials and starts a session.
func (h *Handlers) Login(ctx context.Context, req *oas.LoginRequest) (oas.LoginRes, error) {
	ip := remoteIP(ctx)
	u, err := h.auth.Login(ctx, req.Username, req.Password, ip)
	if err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			if w, ok := writerFrom(ctx); ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(auth.LockoutCooldown.Seconds())))
			}
		}
		return nil, err
	}
	return h.startSession(ctx, u)
}

func remoteIP(ctx context.Context) string {
	r, ok := requestFrom(ctx)
	if !ok || r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// Logout destroys the session cookie and row.
func (h *Handlers) Logout(ctx context.Context) error {
	w, okW := writerFrom(ctx)
	r, okR := requestFrom(ctx)
	if !okW || !okR {
		return fmt.Errorf("%w: no http context", domain.ErrUnauthorized)
	}
	if err := h.sessions.Destroy(w, r); err != nil {
		return fmt.Errorf("httpapi: logout: %w", err)
	}
	return nil
}

// GetSession returns the authenticated user and a CSRF token.
func (h *Handlers) GetSession(ctx context.Context) (*oas.SessionHeaders, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	token := h.csrfToken(ctx)
	return &oas.SessionHeaders{
		XCSRFToken: optString(token),
		Response: oas.Session{
			User: userToOAS(u), CsrfToken: token, ExpiresAt: h.clk.Now().Add(h.cfg.Session.TTL),
		},
	}, nil
}

func (h *Handlers) startSession(ctx context.Context, u domain.User) (*oas.Session, error) {
	w, okW := writerFrom(ctx)
	r, okR := requestFrom(ctx)
	if !okW || !okR {
		return nil, errors.New("httpapi: no http context for session")
	}
	sess, err := h.sessions.Load(r)
	if err != nil {
		return nil, fmt.Errorf("httpapi: load session: %w", err)
	}
	sess.Set(sessionUserKey, u.ID.String())
	if err := h.sessions.SaveContext(ctx, w, sess); err != nil {
		return nil, fmt.Errorf("httpapi: save session: %w", err)
	}
	return &oas.Session{
		User: userToOAS(u), CsrfToken: h.csrfToken(ctx), ExpiresAt: h.clk.Now().Add(h.cfg.Session.TTL),
	}, nil
}

func (h *Handlers) csrfToken(ctx context.Context) string {
	r, ok := requestFrom(ctx)
	if !ok {
		return ""
	}
	return csrf.Token(r)
}
