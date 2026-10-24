// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/settings"
)

// ToolSpecForTest exposes toolSpec for tests in package llm_test.
type ToolSpecForTest = toolSpec

// ModuleCheckerForTest exposes the module checker interface for package llm_test.
type ModuleCheckerForTest = moduleChecker

// RegisteredToolsForTest returns the registered tools slice for tests.
func RegisteredToolsForTest() []toolSpec {
	return toolRegistry
}

// NewWithSpecsForTest constructs a Service using a custom toolSpec slice for testing error paths.
func NewWithSpecsForTest(
	build buildinfo.Info,
	settingsSvc *settings.Service,
	security *httpapi.Security,
	handlers *httpapi.Handlers,
	modulesSvc moduleChecker,
	logger *slog.Logger,
	specs []toolSpec,
) (*Service, error) {
	return newWithSpecs(build, settingsSvc, security, handlers, modulesSvc, logger, specs, nil)
}

// NewWithSpecsAndOpsForTest constructs a Service using custom toolSpec and ops for testing module gating.
func NewWithSpecsAndOpsForTest(
	build buildinfo.Info,
	settingsSvc *settings.Service,
	security *httpapi.Security,
	handlers *httpapi.Handlers,
	modulesSvc moduleChecker,
	logger *slog.Logger,
	specs []toolSpec,
	ops map[string]string,
) (*Service, error) {
	return newWithSpecs(build, settingsSvc, security, handlers, modulesSvc, logger, specs, ops)
}

// PromptInstructionsForTest returns the three prompt instruction constants for tests.
func PromptInstructionsForTest() []string {
	return []string{
		whyNotSnatchedInstructions,
		dailySummaryInstructions,
		capHeadroomInstructions,
	}
}

// WithRequestAuthScopesForTest attaches a requestAuth with the given scopes to ctx for in-handler barrier tests.
func WithRequestAuthScopesForTest(ctx context.Context, user domain.User, scopes ...string) context.Context {
	return withRequestAuth(ctx, requestAuth{user: user, scopes: scopes})
}

// HandleInstancesResourceForTest exposes handleInstancesResource for in-handler barrier tests.
func HandleInstancesResourceForTest(h *httpapi.Handlers) mcp.ResourceHandler {
	return handleInstancesResource(h)
}

// HandleQueueResourceForTest exposes handleQueueResource for in-handler barrier tests.
func HandleQueueResourceForTest(h *httpapi.Handlers) mcp.ResourceHandler {
	return handleQueueResource(h)
}

// HandleHistoryResourceForTest exposes handleHistoryResource for in-handler barrier tests.
func HandleHistoryResourceForTest(h *httpapi.Handlers) mcp.ResourceHandler {
	return handleHistoryResource(h)
}

// HandleWhyNotSnatchedForTest exposes handleWhyNotSnatched for in-handler barrier tests.
func HandleWhyNotSnatchedForTest(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return handleWhyNotSnatched(ctx, req)
}

// HandleDailySummaryForTest exposes handleDailySummary for in-handler barrier tests.
func HandleDailySummaryForTest(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return handleDailySummary(ctx, req)
}

// HandleCapHeadroomForTest exposes handleCapHeadroom for in-handler barrier tests.
func HandleCapHeadroomForTest(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return handleCapHeadroom(ctx, req)
}
