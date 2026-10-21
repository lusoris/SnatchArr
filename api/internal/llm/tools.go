// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
)

type toolCallFunc func(ctx context.Context, h *httpapi.Handlers, req *mcp.CallToolRequest) (any, error)

type toolSpec struct {
	Name        string
	OperationID string
	Scope       string
	ReadOnly    bool
	Destructive bool
	Description string
	Call        toolCallFunc
}

var toolRegistry = []toolSpec{
	{
		Name:        "snatcharr_get_system_status",
		OperationID: "getSystemStatus",
		Scope:       domain.ScopeAdminRead,
		ReadOnly:    true,
		Description: "Return system build, profile, setup, and worker contact status.",
		Call: func(ctx context.Context, h *httpapi.Handlers, _ *mcp.CallToolRequest) (any, error) {
			return h.GetSystemStatus(ctx)
		},
	},
	{
		Name:        "snatcharr_list_instances",
		OperationID: "listInstances",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "List configured *arr instances without secrets.",
		Call: func(ctx context.Context, h *httpapi.Handlers, _ *mcp.CallToolRequest) (any, error) {
			return h.ListInstances(ctx)
		},
	},
}

func lookupTool(name string) (toolSpec, bool) {
	for _, spec := range toolRegistry {
		if spec.Name == name {
			return spec, true
		}
	}
	return toolSpec{}, false
}

func registerTools(srv *mcp.Server, h *httpapi.Handlers, user domain.User, scopes []string) {
	for _, spec := range toolRegistry {
		if !slices.Contains(scopes, spec.Scope) {
			continue
		}
		destructive := spec.Destructive
		tool := &mcp.Tool{
			Name:        spec.Name,
			Description: spec.Description,
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    spec.ReadOnly,
				DestructiveHint: &destructive,
			},
		}
		call := spec.Call
		mcp.AddTool[struct{}, any](srv, tool, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, user), 30*time.Second)
			defer cancel()
			out, err := call(callCtx, h, req)
			if err != nil {
				return nil, nil, err
			}
			return nil, out, nil
		})
	}
}
