// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/golusoris/golusoris/auth/oidc"
	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const maxFlowAge = 10 * time.Minute

// Service coordinates OIDC login flows and session issuance.
type Service struct {
	cfg        config.OIDCOptions
	auth       *auth.Service
	sessions   *session.Manager
	clk        clock.Clock
	logger     *slog.Logger
	httpClient *http.Client

	mu       sync.Mutex
	provider *oidc.Provider
	flow     *oidc.SessionFlow
}

// NewService creates a new SSO service.
func NewService(cfg config.Options, authSvc *auth.Service, sessions *session.Manager, clk clock.Clock, logger *slog.Logger) *Service {
	timeout := cfg.OIDC.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{
		cfg:      cfg.OIDC,
		auth:     authSvc,
		sessions: sessions,
		clk:      clk,
		logger:   logger,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// SetHTTPClient sets the HTTP client used for discovery and token exchange (primarily for tests).
func (s *Service) SetHTTPClient(client *http.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpClient = client
	s.provider = nil
	s.flow = nil
}

func (s *Service) getOrInitFlow(ctx context.Context) (*oidc.SessionFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flow != nil {
		return s.flow, nil
	}
	timeout := s.cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	opts := oidc.Options{
		IssuerURL:            s.cfg.IssuerURL,
		ClientID:             s.cfg.ClientID,
		ClientSecret:         s.cfg.ClientSecret,
		RedirectURL:          s.cfg.RedirectURL,
		Scopes:               s.cfg.Scopes,
		DiscoveryTimeout:     timeout,
		LazyDiscovery:        true,
		DisableOfflineAccess: true,
		HTTPClient:           client,
	}
	prov, err := oidc.NewProvider(ctx, opts, s.logger)
	if err != nil {
		return nil, fmt.Errorf("sso: new oidc provider: %w", err)
	}
	flow, err := oidc.NewSessionFlow(prov, oidc.SessionFlowOptions{
		Sessions:  s.sessions,
		Clock:     s.clk,
		MaxAge:    maxFlowAge,
		OnSuccess: s.onSuccess,
		OnError:   s.onError,
	})
	if err != nil {
		return nil, fmt.Errorf("sso: new oidc flow: %w", err)
	}
	s.provider = prov
	s.flow = flow
	return s.flow, nil
}

// LoginHandler initiates the OIDC authorization code flow with PKCE.
func (s *Service) LoginHandler(onErr func(context.Context, http.ResponseWriter, *http.Request, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Enabled {
			onErr(r.Context(), w, r, domain.ErrNotFound)
			return
		}
		flow, err := s.getOrInitFlow(r.Context())
		if err != nil {
			s.logger.WarnContext(r.Context(), "sso: oidc discovery failed", slog.String("reason", "discovery_failed"))
			http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
			return
		}
		flow.Login(w, r)
	}
}

// CallbackHandler validates the IdP response, trades code for tokens, and logs in the admin.
func (s *Service) CallbackHandler(onErr func(context.Context, http.ResponseWriter, *http.Request, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Enabled {
			onErr(r.Context(), w, r, domain.ErrNotFound)
			return
		}
		flow, err := s.getOrInitFlow(r.Context())
		if err != nil {
			s.logger.WarnContext(r.Context(), "sso: oidc login refused", slog.String("reason", "discovery_failed"))
			http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
			return
		}
		flow.Callback(w, r)
	}
}

func (s *Service) onError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(err.Error(), "discover provider") {
		s.logger.WarnContext(r.Context(), "sso: oidc discovery failed", slog.String("reason", "discovery_failed"))
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		return
	}
	reason, detail := oidcRefusalDetails(err)
	if detail != "" {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused", slog.String("reason", reason), slog.String("detail", detail))
	} else {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused", slog.String("reason", reason))
	}
	http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
}

func (s *Service) onSuccess(w http.ResponseWriter, r *http.Request, sess *session.Session, toks oidc.TokenSet) {
	groups, ok := s.extractGroups(toks)
	if !ok {
		s.refuseUnauthorized(w, r, toks.Subject, 0)
		return
	}
	role, ok := ResolveRole(groups, s.cfg.AdminGroups, s.cfg.RequesterGroups, s.cfg.ViewerGroups)
	if !ok {
		s.refuseUnauthorized(w, r, toks.Subject, len(groups))
		return
	}
	user, reason, uErr := s.resolveUser(r.Context(), role, toks.Subject)
	if uErr != nil {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused", slog.String("reason", reason))
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		return
	}
	origin := auth.SSOOrigin{
		Source:  "oidc",
		Subject: toks.Subject,
		Groups:  groups,
		Role:    role,
	}
	if err := s.loginUser(r.Context(), w, sess, user, origin); err != nil {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused", slog.String("reason", err.Error()))
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Service) refuseUnauthorized(w http.ResponseWriter, r *http.Request, subject string, groupCount int) {
	s.logger.WarnContext(r.Context(), "sso: oidc login refused",
		slog.String("reason", "not_authorized"),
		slog.String("subject", subject),
		slog.Int("groups", groupCount),
	)
	http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
}

func (s *Service) resolveUser(ctx context.Context, role domain.Role, subject string) (domain.User, string, error) {
	if role == domain.RoleAdmin {
		admin, err := s.auth.FirstAdmin(ctx)
		if err != nil {
			return domain.User{}, "no_admin", err
		}
		return admin, "", nil
	}
	externalUser, err := s.auth.ProvisionExternalUser(ctx, "oidc-", subject, role)
	if err != nil {
		return domain.User{}, "setup_incomplete", err
	}
	return externalUser, "", nil
}

func (s *Service) extractGroups(toks oidc.TokenSet) ([]string, bool) {
	claimName := s.cfg.GroupsClaim
	if claimName == "" {
		claimName = "groups"
	}
	var rawClaims map[string]json.RawMessage
	if err := toks.Claims(&rawClaims); err != nil {
		return nil, false
	}
	rawGroup, ok := rawClaims[claimName]
	if !ok {
		return nil, false
	}
	var groups []string
	if err := json.Unmarshal(rawGroup, &groups); err != nil {
		return nil, false
	}
	return groups, true
}

func (s *Service) loginUser(ctx context.Context, w http.ResponseWriter, sess *session.Session, u domain.User, origin auth.SSOOrigin) error {
	if err := s.auth.PopulateSSOSession(ctx, s.sessions, w, sess, u.ID, origin); err != nil {
		return fmt.Errorf("sso: %w", err)
	}
	return nil
}

func oidcRefusalDetails(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if reason, ok := mapFlowError(err); ok {
		return reason, ""
	}
	return mapTokenError(err)
}

func mapFlowError(err error) (string, bool) {
	switch {
	case errors.Is(err, oidc.ErrFlowState):
		return "invalid_state", true
	case errors.Is(err, oidc.ErrFlowExpired):
		return "flow_expired", true
	case errors.Is(err, oidc.ErrFlowIDP):
		return "idp_error", true
	case errors.Is(err, oidc.ErrFlowCode):
		return "missing_code", true
	default:
		return "", false
	}
}

func mapTokenError(err error) (string, string) {
	switch {
	case errors.Is(err, oidc.ErrNonceMismatch):
		return "invalid_nonce", ""
	case errors.Is(err, oidc.ErrExchange):
		return exchangeErrorDetail(err)
	case errors.Is(err, oidc.ErrIDTokenMissing):
		return "missing_id_token", ""
	case errors.Is(err, oidc.ErrIDTokenInvalid):
		return "invalid_id_token", idTokenErrorDetail(err)
	case strings.Contains(err.Error(), "discover provider"):
		return "discovery_failed", ""
	default:
		return err.Error(), ""
	}
}

func exchangeErrorDetail(err error) (string, string) {
	if retErr, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		return "exchange_failed", retErr.ErrorCode
	}
	return "exchange_failed", ""
}

// maxUnwrapDepth bounds the walk to the innermost cause of a verification error.
const maxUnwrapDepth = 16

// idTokenErrorDetail returns the innermost cause of an ID token verification error,
// following the last branch of a joined error.
func idTokenErrorDetail(err error) string {
	type multiUnwrap interface {
		Unwrap() []error
	}
	for range maxUnwrapDepth {
		next := errors.Unwrap(err)
		if mu, ok := err.(multiUnwrap); ok {
			if errs := mu.Unwrap(); len(errs) > 0 {
				next = errs[len(errs)-1]
			}
		}
		if next == nil {
			break
		}
		err = next
	}
	return err.Error()
}

// Mount attaches the OIDC login and callback routes to the router.
// They are mounted as plain chi routes (like MountStream) because ogen does not
// cleanly handle 302 redirects with session-cookie side effects and arbitrary
// IdP query parameter variations, and every refusal path requires a 302 redirect
// to /login?sso=failed rather than an immediate RFC 9457 JSON response.
func Mount(r chi.Router, prefix string, svc *Service, onErr func(context.Context, http.ResponseWriter, *http.Request, error)) {
	r.Get(prefix+"/auth/oidc/login", svc.LoginHandler(onErr))
	r.Get(prefix+"/auth/oidc/callback", svc.CallbackHandler(onErr))
}
