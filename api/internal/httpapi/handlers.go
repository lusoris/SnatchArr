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
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/csrf"

	"github.com/lusoris/SnatchArr/api/internal/adult"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
	"github.com/lusoris/SnatchArr/api/internal/logring"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/notify"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/requests"
	"github.com/lusoris/SnatchArr/api/internal/runtimesettings"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/sso"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

// Handlers implements oas.Handler.
type Handlers struct {
	build           buildinfo.Info
	cfg             config.Options
	clk             clock.Clock
	auth            *auth.Service
	apiKeys         *auth.APIKeys
	sessions        *session.Manager
	instances       *instances.Service
	policies        *policies.Service
	queue           *queue.Store
	queueSvc        queueService
	options         *policies.Options
	logs            *logring.Ring
	runs            *snatch.Runs
	workers         *snatch.Workers
	rec             *snatch.Recorder
	budget          *snatch.Budget
	memory          *snatch.Memory
	planner         *snatch.Planner
	schedules       *snatch.Schedules
	clients         *dlclients.Service
	settings        *settings.Service
	requests        *requests.Service
	modules         *modules.Service
	seerr           *seerr.Service
	prowlarr        *prowlarr.Service
	configarr       *configarr.Service
	notify          *notify.Service
	watchaware      *watchaware.Service
	adult           *adult.Service
	jellyfin        jellyfin.Authenticator
	runtimeSettings *runtimesettings.Service
	logger          *slog.Logger
	qcCache         quickConnectCache
}

type quickConnectCache struct {
	mu        sync.Mutex
	value     bool
	fetchedAt time.Time
}

// Deps groups the snatch services the handlers need.
type Deps struct {
	fx.In
	Runs            *snatch.Runs
	Workers         *snatch.Workers
	Rec             *snatch.Recorder
	Budget          *snatch.Budget
	Memory          *snatch.Memory
	Planner         *snatch.Planner
	Schedules       *snatch.Schedules
	Clients         *dlclients.Service
	Settings        *settings.Service
	Requests        *requests.Service
	Modules         *modules.Service
	Seerr           *seerr.Service
	Prowlarr        *prowlarr.Service
	Configarr       *configarr.Service
	Notify          *notify.Service
	WatchAware      *watchaware.Service
	Adult           *adult.Service
	Jellyfin        jellyfin.Authenticator
	Options         *policies.Options
	Logs            *logring.Ring
	Queue           *queue.Store
	QueueSvc        *queue.Service
	RuntimeSettings *runtimesettings.Service
}

// NewHandlers wires the operation handlers.
func NewHandlers(build buildinfo.Info, cfg config.Options, clk clock.Clock, authSvc *auth.Service,
	apiKeys *auth.APIKeys, sessions *session.Manager, inst *instances.Service, pol *policies.Service, logger *slog.Logger, d Deps,
) *Handlers {
	jf := d.Jellyfin
	if jf == nil {
		jf, _ = jellyfin.ProvideAuthenticator(config.Options{})
	}
	return &Handlers{
		build: build, cfg: cfg, clk: clk, auth: authSvc, apiKeys: apiKeys, sessions: sessions, instances: inst, policies: pol,
		queue: d.Queue, queueSvc: d.QueueSvc, runs: d.Runs, workers: d.Workers, rec: d.Rec, budget: d.Budget, memory: d.Memory, planner: d.Planner, schedules: d.Schedules, clients: d.Clients, settings: d.Settings, requests: d.Requests, modules: d.Modules, seerr: d.Seerr, prowlarr: d.Prowlarr, configarr: d.Configarr, notify: d.Notify, watchaware: d.WatchAware, adult: d.Adult, jellyfin: jf, options: d.Options, logs: d.Logs, runtimeSettings: d.RuntimeSettings, logger: logger,
	}
}

var _ oas.Handler = (*Handlers)(nil)

// NewError renders handler errors as RFC 9457 (ogen "convenient error" hook).
func (h *Handlers) NewError(_ context.Context, err error) *oas.ProblemStatusCode {
	p := resolver.Resolve(nil, err)
	items := make([]oas.ProblemInvalidMinusParamsItem, 0, len(p.InvalidParams))
	for _, ip := range p.InvalidParams {
		items = append(items, oas.ProblemInvalidMinusParamsItem{Name: ip.Name, Reason: ip.Reason})
	}
	return &oas.ProblemStatusCode{
		StatusCode: p.Status,
		Response: oas.Problem{
			Type:               url.URL{Scheme: "about", Opaque: "blank"},
			Title:              p.Title,
			Status:             p.Status,
			Detail:             optString(p.Detail),
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
	return h.startSession(ctx, u, nil)
}

// ChangePassword changes the current user's password, ends every session of the user and
// signs the caller in again under a fresh session id (#268).
func (h *Handlers) ChangePassword(ctx context.Context, req *oas.ChangePasswordRequest) (oas.ChangePasswordRes, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	w, okW := writerFrom(ctx)
	r, okR := requestFrom(ctx)
	if !okW || !okR {
		return nil, fmt.Errorf("%w: no http context", domain.ErrUnauthorized)
	}
	if err := h.auth.ChangePassword(ctx, u.ID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			w.Header().Set("Retry-After", strconv.Itoa(int(auth.LockoutCooldown.Seconds())))
		}
		return nil, err
	}
	if err := h.auth.RenewSession(ctx, h.sessions, w, r, u.ID); err != nil {
		return nil, fmt.Errorf("httpapi: %w", err)
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
	return h.startSession(ctx, u, nil)
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

func (h *Handlers) startSession(ctx context.Context, u domain.User, origin *auth.SSOOrigin) (*oas.Session, error) {
	w, okW := writerFrom(ctx)
	r, okR := requestFrom(ctx)
	if !okW || !okR {
		return nil, errors.New("httpapi: no http context for session")
	}
	var err error
	if origin != nil {
		u.Role = origin.Role
		err = h.auth.RenewSSOSession(ctx, h.sessions, w, r, u.ID, *origin)
	} else {
		err = h.auth.RenewSession(ctx, h.sessions, w, r, u.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("httpapi: %w", err)
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

const quickConnectCacheTTL = 60 * time.Second

// GetSSO reports whether OIDC, proxy, or Jellyfin authentication is enabled.
func (h *Handlers) GetSSO(ctx context.Context) (*oas.SSOStatus, error) {
	status := &oas.SSOStatus{
		Oidc:     h.cfg.OIDC.Enabled,
		Proxy:    h.cfg.ProxyAuth.Enabled,
		Jellyfin: h.cfg.Jellyfin.Enabled,
	}
	if !h.cfg.Jellyfin.Enabled {
		status.JellyfinQuickConnect = oas.NewOptBool(false)
		return status, nil
	}
	qcEnabled := h.quickConnectEnabled(ctx)
	status.JellyfinQuickConnect = oas.NewOptBool(qcEnabled)
	return status, nil
}

func (h *Handlers) quickConnectEnabled(ctx context.Context) bool {
	h.qcCache.mu.Lock()
	defer h.qcCache.mu.Unlock()

	now := h.clk.Now()
	if !h.qcCache.fetchedAt.IsZero() && now.Sub(h.qcCache.fetchedAt) < quickConnectCacheTTL {
		return h.qcCache.value
	}
	enabled, err := h.jellyfin.QuickConnectEnabled(ctx)
	h.qcCache.fetchedAt = now
	if err != nil {
		if h.logger != nil {
			h.logger.WarnContext(ctx, "httpapi: check jellyfin quick connect enabled failed", slog.String("error", err.Error()))
		}
		h.qcCache.value = false
		return false
	}
	h.qcCache.value = enabled
	return enabled
}

func (h *Handlers) resolveProxyAccount(ctx context.Context, user string, role domain.Role) (domain.User, error) {
	if role == domain.RoleAdmin {
		admin, err := h.auth.FirstAdmin(ctx)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.User{}, fmt.Errorf("%w: no admin user configured", domain.ErrConflict)
			}
			return domain.User{}, err
		}
		return admin, nil
	}
	linkedUser, ok, err := h.auth.LinkedUser(ctx, domain.IdentityProviderProxy, user)
	if err != nil {
		return domain.User{}, err
	}
	if ok {
		return linkedUser, nil
	}
	return h.auth.ProvisionExternalUser(ctx, "proxy-", user, role)
}

// CreateProxySession starts a session when proxy auth is enabled and verified.
func (h *Handlers) CreateProxySession(ctx context.Context) (oas.CreateProxySessionRes, error) {
	if !h.cfg.ProxyAuth.Enabled {
		return nil, domain.ErrNotFound
	}
	user, ok := sso.ProxyUserFromContext(ctx)
	if !ok || user == "" {
		return nil, domain.ErrUnauthorized
	}
	groups, _ := sso.ProxyGroupsFromContext(ctx)
	role, ok := sso.ResolveRole(groups, h.cfg.ProxyAuth.AdminGroups, h.cfg.ProxyAuth.RequesterGroups, h.cfg.ProxyAuth.ViewerGroups)
	if !ok {
		h.logger.WarnContext(ctx, "sso: proxy login refused",
			slog.String("reason", "not_authorized"),
			slog.String("subject", user),
			slog.Int("groups", len(groups)),
		)
		return nil, domain.ErrForbidden
	}
	origin := auth.SSOOrigin{
		Source:  "proxy",
		Subject: user,
		Groups:  groups,
		Role:    role,
	}
	targetUser, err := h.resolveProxyAccount(ctx, user, role)
	if err != nil {
		return nil, err
	}
	return h.startSession(ctx, targetUser, &origin)
}
