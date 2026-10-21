// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package llm serves the SnatchArr Model Context Protocol (MCP) Streamable HTTP endpoint.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/settings"
)

// Path is the route where the Streamable HTTP MCP server is mounted.
const Path = httpapi.PathPrefix + "/mcp"

const maxBodyBytes = 1 << 20

type scopesKey struct{}

func withKeyScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesKey{}, scopes)
}

func keyScopesFrom(ctx context.Context) []string {
	scopes, _ := ctx.Value(scopesKey{}).([]string)
	return scopes
}

// Service wraps the stateless MCP Streamable HTTP handler and its security/toggle gates.
type Service struct {
	build      buildinfo.Info
	settings   *settings.Service
	security   *httpapi.Security
	handlers   *httpapi.Handlers
	logger     *slog.Logger
	mcpHandler http.Handler
}

// New constructs the MCP service.
func New(build buildinfo.Info, settingsSvc *settings.Service, security *httpapi.Security, handlers *httpapi.Handlers, logger *slog.Logger) *Service {
	s := &Service{
		build:    build,
		settings: settingsSvc,
		security: security,
		handlers: handlers,
		logger:   logger,
	}
	s.mcpHandler = mcp.NewStreamableHTTPHandler(s.getServer, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return s
}

// Module provides the MCP service to the fx graph.
var Module = fx.Module("snatcharr.llm",
	fx.Provide(New),
)

// Mount attaches the MCP endpoint to r at Path.
func Mount(r chi.Router, svc *Service) {
	r.Handle(Path, svc.Handler())
}

// Handler returns the guarded HTTP handler for /api/v1/mcp.
func (s *Service) Handler() http.Handler {
	onErr := httpapi.ErrorHandler(s.logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkEnabled(r.Context()); err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		if err := checkSameOrigin(r); err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		user, scopes, err := s.authenticateBearer(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			onErr(r.Context(), w, r, err)
			return
		}
		if len(scopes) == 0 {
			onErr(r.Context(), w, r, fmt.Errorf("%w: unscoped api keys are not permitted on mcp", domain.ErrForbidden))
			return
		}
		if missingScope, err := inspectToolCallScope(r, scopes); err != nil {
			if missingScope != "" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, missingScope))
			}
			onErr(r.Context(), w, r, err)
			return
		}
		ctx := httpapi.WithUser(r.Context(), user)
		ctx = withKeyScopes(ctx, scopes)
		s.mcpHandler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) checkEnabled(ctx context.Context) error {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg, err := s.settings.Get(qCtx)
	if err != nil {
		return fmt.Errorf("llm: get settings: %w", err)
	}
	if !cfg.LLMEnabled {
		return fmt.Errorf("%w: llm module is disabled", domain.ErrNotFound)
	}
	return nil
}

func checkSameOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || r.Host == "" || !strings.EqualFold(u.Host, r.Host) {
		return fmt.Errorf("%w: cross-origin request refused", domain.ErrForbidden)
	}
	return nil
}

func (s *Service) authenticateBearer(r *http.Request) (domain.User, []string, error) {
	authCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	user, key, err := s.security.VerifyBearerRequest(authCtx, r)
	if err != nil {
		return domain.User{}, nil, err
	}
	return user, key.Scopes, nil
}

type rpcToolCallPeek struct {
	Method string `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

func inspectToolCallScope(r *http.Request, scopes []string) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	// net/http closes the original body after the handler returns.
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	r.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("%w: read request body: %w", domain.ErrBadRequest, err)
	}
	if len(buf) > maxBodyBytes {
		return "", fmt.Errorf("%w: request body exceeds 1 MiB limit", domain.ErrBadRequest)
	}
	name := calledToolName(buf)
	if name == "" {
		return "", nil
	}
	spec, ok := lookupTool(name)
	if !ok || slices.Contains(scopes, spec.Scope) {
		return "", nil
	}
	return spec.Scope, fmt.Errorf("%w: key lacks required scope %s", domain.ErrForbidden, spec.Scope)
}

// calledToolName returns the tool a tools/call request names, or "" for
// any other request. Malformed JSON yields "": the SDK answers it with a
// parse error.
func calledToolName(buf []byte) string {
	var peek rpcToolCallPeek
	if err := json.Unmarshal(buf, &peek); err != nil || peek.Method != "tools/call" {
		return ""
	}
	return peek.Params.Name
}

func (s *Service) getServer(r *http.Request) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    buildinfo.Name,
		Version: s.build.Version,
	}, nil)
	user, _ := httpapi.UserFrom(r.Context())
	scopes := keyScopesFrom(r.Context())
	registerTools(srv, s.handlers, user, scopes)
	return srv
}
