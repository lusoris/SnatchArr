// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const (
	sessionStateKey    = "oidc_state"
	sessionNonceKey    = "oidc_nonce"
	sessionVerifierKey = "oidc_verifier"
	sessionIssuedAtKey = "oidc_issued_at"
	maxFlowAge         = 10 * time.Minute
)

// Service coordinates OIDC login flows and session issuance.
type Service struct {
	cfg        config.OIDCOptions
	auth       *auth.Service
	sessions   *session.Manager
	clk        clock.Clock
	logger     *slog.Logger
	httpClient *http.Client

	mu        sync.Mutex
	provider  *gooidc.Provider
	verifier  *gooidc.IDTokenVerifier
	oauth2Cfg *oauth2.Config
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
	s.verifier = nil
	s.oauth2Cfg = nil
}

func (s *Service) getOrInitProvider(ctx context.Context) (*oauth2.Config, *gooidc.IDTokenVerifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth2Cfg != nil && s.verifier != nil {
		return s.oauth2Cfg, s.verifier, nil
	}
	timeout := s.cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	dCtx = gooidc.ClientContext(dCtx, client)

	prov, err := gooidc.NewProvider(dCtx, s.cfg.IssuerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("sso: oidc discovery: %w", err)
	}
	verifier := prov.Verifier(&gooidc.Config{ClientID: s.cfg.ClientID})
	oauth2Cfg := &oauth2.Config{
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		RedirectURL:  s.cfg.RedirectURL,
		Endpoint:     prov.Endpoint(),
		Scopes:       s.cfg.Scopes,
	}
	s.provider = prov
	s.verifier = verifier
	s.oauth2Cfg = oauth2Cfg
	return s.oauth2Cfg, s.verifier, nil
}

// LoginHandler initiates the OIDC authorization code flow with PKCE.
func (s *Service) LoginHandler(onErr func(context.Context, http.ResponseWriter, *http.Request, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Enabled {
			onErr(r.Context(), w, r, domain.ErrNotFound)
			return
		}
		oauth2Cfg, _, err := s.getOrInitProvider(r.Context())
		if err != nil {
			s.logger.WarnContext(r.Context(), "sso: oidc discovery failed", slog.String("reason", "discovery_failed"))
			http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
			return
		}
		state, sErr := randomToken()
		nonce, nErr := randomToken()
		verifier, vErr := randomToken()
		if err := errors.Join(sErr, nErr, vErr); err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		if err := s.saveOIDCState(r.Context(), w, r, state, nonce, verifier); err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		challenge := pkceChallenge(verifier)
		authURL := oauth2Cfg.AuthCodeURL(
			state,
			gooidc.Nonce(nonce),
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func (s *Service) saveOIDCState(ctx context.Context, w http.ResponseWriter, r *http.Request, state, nonce, verifier string) error {
	sess, err := s.sessions.Load(r)
	if err != nil {
		return fmt.Errorf("sso: load session: %w", err)
	}
	sess.Set(sessionStateKey, state)
	sess.Set(sessionNonceKey, nonce)
	sess.Set(sessionVerifierKey, verifier)
	sess.Set(sessionIssuedAtKey, s.clk.Now().Unix())
	if err := s.sessions.SaveContext(ctx, w, sess); err != nil {
		return fmt.Errorf("sso: save session: %w", err)
	}
	return nil
}

// CallbackHandler validates the IdP response, trades code for tokens, and logs in the admin.
func (s *Service) CallbackHandler(onErr func(context.Context, http.ResponseWriter, *http.Request, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Enabled {
			onErr(r.Context(), w, r, domain.ErrNotFound)
			return
		}
		state, nonce, verifier, issuedAt, err := s.consumeOIDCState(r.Context(), w, r)
		if err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		refuse := func(ctx context.Context, reason, detail string) {
			if detail != "" {
				s.logger.WarnContext(ctx, "sso: oidc login refused", slog.String("reason", reason), slog.String("detail", detail))
			} else {
				s.logger.WarnContext(ctx, "sso: oidc login refused", slog.String("reason", reason))
			}
			http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		}
		code, reason := s.validateCallbackParams(r, state, verifier, issuedAt)
		if reason != "" {
			refuse(r.Context(), reason, "")
			return
		}
		s.completeCallback(w, r, code, nonce, verifier, refuse, onErr)
	}
}

func (s *Service) validateCallbackParams(r *http.Request, state, verifier string, issuedAt time.Time) (string, string) {
	if r.URL.Query().Get("error") != "" {
		return "", "idp_error"
	}
	paramState := r.URL.Query().Get("state")
	if paramState == "" || state == "" || subtle.ConstantTimeCompare([]byte(paramState), []byte(state)) != 1 {
		return "", "invalid_state"
	}
	if issuedAt.IsZero() || s.clk.Now().Sub(issuedAt) > maxFlowAge {
		return "", "flow_expired"
	}
	code := r.URL.Query().Get("code")
	if code == "" || verifier == "" {
		return "", "missing_code"
	}
	return code, ""
}

func (s *Service) completeCallback(w http.ResponseWriter, r *http.Request, code, nonce, verifier string,
	refuse func(context.Context, string, string), onErr func(context.Context, http.ResponseWriter, *http.Request, error),
) {
	oauth2Cfg, idTokenVerifier, err := s.getOrInitProvider(r.Context())
	if err != nil {
		refuse(r.Context(), "discovery_failed", "")
		return
	}
	idToken, err := s.exchangeAndVerify(r.Context(), oauth2Cfg, idTokenVerifier, code, verifier)
	if err != nil {
		reason, detail := oidcRefusalDetails(err)
		refuse(r.Context(), reason, detail)
		return
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		refuse(r.Context(), "invalid_nonce", "")
		return
	}
	groups, ok := s.extractGroups(idToken)
	if !ok {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused",
			slog.String("reason", "not_authorized"),
			slog.String("subject", idToken.Subject),
			slog.Int("groups", 0),
		)
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		return
	}
	role, ok := ResolveRole(groups, s.cfg.AdminGroups, s.cfg.ViewerGroups)
	if !ok {
		s.logger.WarnContext(r.Context(), "sso: oidc login refused",
			slog.String("reason", "not_authorized"),
			slog.String("subject", idToken.Subject),
			slog.Int("groups", len(groups)),
		)
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
		return
	}
	user, reason, uErr := s.resolveUser(r.Context(), role, idToken.Subject)
	if uErr != nil {
		refuse(r.Context(), reason, "")
		return
	}
	if err := s.loginUser(r.Context(), w, r, user); err != nil {
		onErr(r.Context(), w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Service) resolveUser(ctx context.Context, role domain.Role, subject string) (domain.User, string, error) {
	if role == domain.RoleAdmin {
		admin, err := s.auth.FirstAdmin(ctx)
		if err != nil {
			return domain.User{}, "no_admin", err
		}
		return admin, "", nil
	}
	viewer, err := s.auth.ProvisionExternalViewer(ctx, "oidc-", subject)
	if err != nil {
		return domain.User{}, "setup_incomplete", err
	}
	return viewer, "", nil
}

func (s *Service) exchangeAndVerify(ctx context.Context, oauth2Cfg *oauth2.Config, verifier *gooidc.IDTokenVerifier, code, verifierCode string) (*gooidc.IDToken, error) {
	timeout := s.cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	exCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	exCtx = gooidc.ClientContext(exCtx, client)

	token, err := oauth2Cfg.Exchange(exCtx, code, oauth2.SetAuthURLParam("code_verifier", verifierCode))
	if err != nil {
		return nil, fmt.Errorf("exchange_failed: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("missing_id_token")
	}
	idToken, err := verifier.Verify(exCtx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("invalid_id_token: %w", err)
	}
	return idToken, nil
}

func oidcRefusalDetails(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if retErr, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		return "exchange_failed", retErr.ErrorCode
	}
	if strings.HasPrefix(err.Error(), "exchange_failed") {
		return "exchange_failed", ""
	}
	if strings.HasPrefix(err.Error(), "invalid_id_token") {
		cause := errors.Unwrap(err)
		if cause != nil {
			return "invalid_id_token", cause.Error()
		}
		return "invalid_id_token", err.Error()
	}
	if strings.HasPrefix(err.Error(), "missing_id_token") {
		return "missing_id_token", ""
	}
	return err.Error(), ""
}

func (s *Service) extractGroups(idToken *gooidc.IDToken) ([]string, bool) {
	claimName := s.cfg.GroupsClaim
	if claimName == "" {
		claimName = "groups"
	}
	var rawClaims map[string]json.RawMessage
	if err := idToken.Claims(&rawClaims); err != nil {
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

func (s *Service) loginUser(ctx context.Context, w http.ResponseWriter, r *http.Request, u domain.User) error {
	// The pre-login session held the OIDC state; the user gets a fresh id (#268).
	if err := s.auth.RenewSession(ctx, s.sessions, w, r, u.ID); err != nil {
		return fmt.Errorf("sso: %w", err)
	}
	return nil
}

func (s *Service) consumeOIDCState(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, string, string, time.Time, error) {
	sess, err := s.sessions.Load(r)
	if err != nil {
		return "", "", "", time.Time{}, fmt.Errorf("sso: load session: %w", err)
	}
	state, _ := sess.Get(sessionStateKey).(string)
	nonce, _ := sess.Get(sessionNonceKey).(string)
	verifier, _ := sess.Get(sessionVerifierKey).(string)
	rawIssued := sess.Get(sessionIssuedAtKey)

	sess.Delete(sessionStateKey)
	sess.Delete(sessionNonceKey)
	sess.Delete(sessionVerifierKey)
	sess.Delete(sessionIssuedAtKey)

	if err := s.sessions.SaveContext(ctx, w, sess); err != nil {
		return "", "", "", time.Time{}, fmt.Errorf("sso: save session: %w", err)
	}
	return state, nonce, verifier, parseUnixTime(rawIssued), nil
}

func parseUnixTime(v any) time.Time {
	switch val := v.(type) {
	case int64:
		return time.Unix(val, 0)
	case float64:
		return time.Unix(int64(val), 0)
	case int:
		return time.Unix(int64(val), 0)
	default:
		return time.Time{}
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sso: crypto rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
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
