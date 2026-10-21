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

// ErrCodeToolNotEnabled is the JSON-RPC error code returned when a write tool is not opted in.
const ErrCodeToolNotEnabled = -32021

const maxBodyBytes = 1 << 20

type authCtxKey struct{}

type requestAuth struct {
	user       domain.User
	keyID      string
	scopes     []string
	writeTools []string
}

func withRequestAuth(ctx context.Context, auth requestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey{}, auth)
}

func requestAuthFrom(ctx context.Context) requestAuth {
	auth, _ := ctx.Value(authCtxKey{}).(requestAuth)
	return auth
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
		cfg, err := s.loadEnabledSettings(r.Context())
		if err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		if err = checkSameOrigin(r); err != nil {
			onErr(r.Context(), w, r, err)
			return
		}
		user, keyID, scopes, err := s.authenticateBearer(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			onErr(r.Context(), w, r, err)
			return
		}
		if len(scopes) == 0 {
			onErr(r.Context(), w, r, fmt.Errorf("%w: unscoped api keys are not permitted on mcp", domain.ErrForbidden))
			return
		}
		auth := requestAuth{user: user, keyID: keyID, scopes: scopes, writeTools: cfg.LLMWriteTools}
		if !s.checkToolCallFences(w, r, auth, onErr) {
			return
		}
		ctx := withRequestAuth(httpapi.WithUser(r.Context(), user), auth)
		s.mcpHandler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) loadEnabledSettings(ctx context.Context) (domain.Settings, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg, err := s.settings.Get(qCtx)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("llm: get settings: %w", err)
	}
	if !cfg.LLMEnabled {
		return domain.Settings{}, fmt.Errorf("%w: llm module is disabled", domain.ErrNotFound)
	}
	return cfg, nil
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

func (s *Service) authenticateBearer(r *http.Request) (domain.User, string, []string, error) {
	authCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	user, key, err := s.security.VerifyBearerRequest(authCtx, r)
	if err != nil {
		return domain.User{}, "", nil, err
	}
	return user, key.ID, key.Scopes, nil
}

type rpcToolCallPeek struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

type jsonRPCErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   jsonRPCError    `json:"error"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	// net/http closes the original body after the handler returns.
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	r.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("%w: read request body: %w", domain.ErrBadRequest, err)
	}
	if len(buf) > maxBodyBytes {
		return nil, fmt.Errorf("%w: request body exceeds 1 MiB limit", domain.ErrBadRequest)
	}
	return buf, nil
}

func (s *Service) checkToolCallFences(w http.ResponseWriter, r *http.Request, auth requestAuth, onErr func(context.Context, http.ResponseWriter, *http.Request, error)) bool {
	buf, err := readRequestBody(r)
	if err != nil {
		onErr(r.Context(), w, r, err)
		return false
	}
	var peek rpcToolCallPeek
	if len(buf) == 0 || json.Unmarshal(buf, &peek) != nil || peek.Method != "tools/call" || peek.Params.Name == "" {
		return true
	}
	spec, ok := lookupTool(peek.Params.Name)
	if !ok {
		return true
	}
	if !slices.Contains(auth.scopes, spec.Scope) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, spec.Scope))
		onErr(r.Context(), w, r, fmt.Errorf("%w: key lacks required scope %s", domain.ErrForbidden, spec.Scope))
		return false
	}
	if !spec.ReadOnly && !slices.Contains(auth.writeTools, spec.Name) {
		s.logger.WarnContext(r.Context(), "llm: write tool refused",
			slog.String("tool", spec.Name),
			slog.String("owner", auth.user.ID.String()),
			slog.String("key_id", auth.keyID),
			slog.String("reason", "not_opted_in"),
		)
		writeJSONRPCError(r.Context(), s.logger, w, peek.ID, ErrCodeToolNotEnabled, "tool not enabled by administrator")
		return false
	}
	return true
}

func writeJSONRPCError(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(jsonRPCErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   jsonRPCError{Code: code, Message: msg},
	})
	if err != nil {
		logger.WarnContext(ctx, "llm: write json-rpc error response", slog.Any("error", err))
	}
}

func (s *Service) getServer(r *http.Request) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    buildinfo.Name,
		Version: s.build.Version,
	}, nil)
	auth := requestAuthFrom(r.Context())
	registerTools(registerContext{
		srv:    srv,
		h:      s.handlers,
		logger: s.logger,
		user:   auth.user,
		keyID:  auth.keyID,
	}, auth.scopes, auth.writeTools)
	return srv
}
