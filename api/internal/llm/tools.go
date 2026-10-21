// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
)

const callTimeout = 30 * time.Second

type idInput struct {
	ID string `json:"id" jsonschema:"UUID identifier"`
}

type listRunsInput struct {
	InstanceID *string `json:"instance_id,omitempty" jsonschema:"Filter runs by instance UUID"`
	Limit      *int    `json:"limit,omitempty" jsonschema:"Maximum number of runs to return (1-500, default 50)"`
	Offset     *int    `json:"offset,omitempty" jsonschema:"Number of runs to skip (default 0)"`
}

type listEventsInput struct {
	InstanceID *string `json:"instance_id,omitempty" jsonschema:"Filter events by instance UUID"`
	BeforeID   *int64  `json:"before_id,omitempty" jsonschema:"Cursor: return events older than this ID"`
	Type       *string `json:"type,omitempty" jsonschema:"Filter events by event type"`
	PageSize   *int    `json:"page_size,omitempty" jsonschema:"Maximum events to return (1-1000, default 100)"`
}

type listLogsInput struct {
	Level  *string `json:"level,omitempty" jsonschema:"Minimum log level (debug, info, warn, error)"`
	Q      *string `json:"q,omitempty" jsonschema:"Filter logs by substring in message or attributes"`
	Source *string `json:"source,omitempty" jsonschema:"Filter logs by source (api, worker, or worker:<id>)"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum log lines to return (1-1000, default 200)"`
}

type toolSpec struct {
	Name        string
	OperationID string
	Scope       string
	ReadOnly    bool
	Destructive bool
	Description string
	Register    func(srv *mcp.Server, h *httpapi.Handlers, user domain.User, tool *mcp.Tool)
}

func toolError(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

func formatProblem(p oas.Problem) string {
	if p.Detail.Set && p.Detail.Value != "" {
		if p.Title != "" {
			return p.Title + ": " + p.Detail.Value
		}
		return p.Detail.Value
	}
	if p.Title != "" {
		return p.Title
	}
	return "operation failed"
}

func handleResult(ctx context.Context, h *httpapi.Handlers, res any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		prob := h.NewError(ctx, err)
		return toolError(formatProblem(prob.Response))
	}
	switch v := res.(type) {
	case *oas.ProblemStatusCode:
		return toolError(formatProblem(v.Response))
	case *oas.Problem:
		return toolError(formatProblem(*v))
	default:
		return nil, res, nil
	}
}

func registerParamTool[In any](convertAndCall func(ctx context.Context, h *httpapi.Handlers, in In) (any, error)) func(srv *mcp.Server, h *httpapi.Handlers, user domain.User, tool *mcp.Tool) {
	return func(srv *mcp.Server, h *httpapi.Handlers, user domain.User, tool *mcp.Tool) {
		mcp.AddTool[In, any](srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, user), callTimeout)
			defer cancel()
			out, err := convertAndCall(callCtx, h, in)
			return handleResult(callCtx, h, out, err)
		})
	}
}

func registerNoParam(call func(ctx context.Context, h *httpapi.Handlers) (any, error)) func(srv *mcp.Server, h *httpapi.Handlers, user domain.User, tool *mcp.Tool) {
	return registerParamTool(func(ctx context.Context, h *httpapi.Handlers, _ struct{}) (any, error) {
		return call(ctx, h)
	})
}

func registerIDParam(call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error)) func(srv *mcp.Server, h *httpapi.Handlers, user domain.User, tool *mcp.Tool) {
	return registerParamTool(func(ctx context.Context, h *httpapi.Handlers, in idInput) (any, error) {
		parsed, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid id %q: %w", domain.ErrBadRequest, in.ID, err)
		}
		return call(ctx, h, parsed)
	})
}

func convertListRunsParams(in listRunsInput) (oas.ListRunsParams, error) {
	var params oas.ListRunsParams
	if in.InstanceID != nil {
		parsed, err := uuid.Parse(*in.InstanceID)
		if err != nil {
			return params, fmt.Errorf("%w: invalid instance_id %q: %w", domain.ErrBadRequest, *in.InstanceID, err)
		}
		params.InstanceID = oas.NewOptUUID(parsed)
	}
	if in.Limit != nil {
		if *in.Limit < 1 || *in.Limit > 500 {
			return params, fmt.Errorf("%w: limit must be between 1 and 500", domain.ErrBadRequest)
		}
		params.Limit = oas.NewOptInt(*in.Limit)
	}
	if in.Offset != nil {
		if *in.Offset < 0 {
			return params, fmt.Errorf("%w: offset must be non-negative", domain.ErrBadRequest)
		}
		params.Offset = oas.NewOptInt(*in.Offset)
	}
	return params, nil
}

func convertListEventsParams(in listEventsInput) (oas.ListEventsParams, error) {
	var params oas.ListEventsParams
	if in.InstanceID != nil {
		parsed, err := uuid.Parse(*in.InstanceID)
		if err != nil {
			return params, fmt.Errorf("%w: invalid instance_id %q: %w", domain.ErrBadRequest, *in.InstanceID, err)
		}
		params.InstanceID = oas.NewOptUUID(parsed)
	}
	if in.BeforeID != nil {
		params.BeforeID = oas.NewOptInt64(*in.BeforeID)
	}
	if in.Type != nil {
		params.Type = oas.NewOptString(*in.Type)
	}
	if in.PageSize != nil {
		if *in.PageSize < 1 || *in.PageSize > 1000 {
			return params, fmt.Errorf("%w: page_size must be between 1 and 1000", domain.ErrBadRequest)
		}
		params.PageSize = oas.NewOptInt(*in.PageSize)
	}
	return params, nil
}

func convertListLogsParams(in listLogsInput) (oas.ListLogsParams, error) {
	var params oas.ListLogsParams
	if in.Level != nil {
		lvl := oas.LogLevel(*in.Level)
		switch lvl {
		case oas.LogLevelDebug, oas.LogLevelInfo, oas.LogLevelWarn, oas.LogLevelError:
			params.Level = oas.NewOptLogLevel(lvl)
		default:
			return params, fmt.Errorf("%w: invalid log level %q", domain.ErrBadRequest, *in.Level)
		}
	}
	if in.Q != nil {
		params.Q = oas.NewOptString(*in.Q)
	}
	if in.Source != nil {
		params.Source = oas.NewOptString(*in.Source)
	}
	if in.Limit != nil {
		if *in.Limit < 1 || *in.Limit > 1000 {
			return params, fmt.Errorf("%w: limit must be between 1 and 1000", domain.ErrBadRequest)
		}
		params.Limit = oas.NewOptInt(*in.Limit)
	}
	return params, nil
}

var toolRegistry = []toolSpec{
	{
		Name:        "snatcharr_get_system_status",
		OperationID: "getSystemStatus",
		Scope:       domain.ScopeAdminRead,
		ReadOnly:    true,
		Description: "Service status and build information.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetSystemStatus(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_setup_status",
		OperationID: "getSetupStatus",
		Scope:       domain.ScopeAdminRead,
		ReadOnly:    true,
		Description: "Whether the first user still has to be created.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetSetupStatus(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_instances",
		OperationID: "listInstances",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "List instances (API keys are never returned).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListInstances(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_instance",
		OperationID: "getInstance",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Get one instance.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_policy",
		OperationID: "getPolicy",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Snatch policy of an instance.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetPolicy(ctx, oas.GetPolicyParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_policy_filter_options",
		OperationID: "getPolicyFilterOptions",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Tags and quality profiles the instance offers for the policy filters.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetPolicyFilterOptions(ctx, oas.GetPolicyFilterOptionsParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_queue_policy",
		OperationID: "getQueuePolicy",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Queue management policy of an instance.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetQueuePolicy(ctx, oas.GetQueuePolicyParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_list_queue_strikes",
		OperationID: "listQueueStrikes",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Active queue strikes of an instance.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.ListQueueStrikes(ctx, oas.ListQueueStrikesParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_list_runs",
		OperationID: "listRuns",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Recent snatch runs, newest first.",
		Register: registerParamTool(func(ctx context.Context, h *httpapi.Handlers, in listRunsInput) (any, error) {
			params, err := convertListRunsParams(in)
			if err != nil {
				return nil, err
			}
			return h.ListRuns(ctx, params)
		}),
	},
	{
		Name:        "snatcharr_get_run",
		OperationID: "getRun",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Load a single snatch run by ID.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetRun(ctx, oas.GetRunParams{RunId: id})
		}),
	},
	{
		Name:        "snatcharr_list_events",
		OperationID: "listEvents",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Snatch history, newest first (cursor on before_id).",
		Register: registerParamTool(func(ctx context.Context, h *httpapi.Handlers, in listEventsInput) (any, error) {
			params, err := convertListEventsParams(in)
			if err != nil {
				return nil, err
			}
			return h.ListEvents(ctx, params)
		}),
	},
	{
		Name:        "snatcharr_get_settings",
		OperationID: "getSettings",
		Scope:       domain.ScopeAdminRead,
		ReadOnly:    true,
		Description: "Runtime settings.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetSettings(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_logs",
		OperationID: "listLogs",
		Scope:       domain.ScopeAdminRead,
		ReadOnly:    true,
		Description: "Recent log records of this api process and its workers, newest first (admin only).",
		Register: registerParamTool(func(ctx context.Context, h *httpapi.Handlers, in listLogsInput) (any, error) {
			params, err := convertListLogsParams(in)
			if err != nil {
				return nil, err
			}
			return h.ListLogs(ctx, params)
		}),
	},
	{
		Name:        "snatcharr_get_hourly_caps",
		OperationID: "getHourlyCaps",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Per-instance stamina spent in the last 60 minutes (rolling window).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetHourlyCaps(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_schedules",
		OperationID: "listSchedules",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "All schedule windows.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListSchedules(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_download_clients",
		OperationID: "listDownloadClients",
		Scope:       domain.ScopeDownloadClientsRead,
		ReadOnly:    true,
		Description: "List download clients (secrets are never returned).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListDownloadClients(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_download_client_status",
		OperationID: "getDownloadClientStatus",
		Scope:       domain.ScopeDownloadClientsRead,
		ReadOnly:    true,
		Description: "Latest observation of every enabled download client (cached 20 s).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetDownloadClientStatus(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_download_client",
		OperationID: "getDownloadClient",
		Scope:       domain.ScopeDownloadClientsRead,
		ReadOnly:    true,
		Description: "Get one download client.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetDownloadClient(ctx, oas.GetDownloadClientParams{ClientId: id})
		}),
	},
	{
		Name:        "snatcharr_list_seerr_links",
		OperationID: "listSeerrLinks",
		Scope:       domain.ScopeRequestsRead,
		ReadOnly:    true,
		Description: "List Seerr links (API keys are never returned).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListSeerrLinks(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_seerr_requests",
		OperationID: "listSeerrRequests",
		Scope:       domain.ScopeRequestsRead,
		ReadOnly:    true,
		Description: "Cached approved-but-unavailable requests with their snatch status.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListSeerrRequests(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_seerr_link",
		OperationID: "getSeerrLink",
		Scope:       domain.ScopeRequestsRead,
		ReadOnly:    true,
		Description: "Get one Seerr link.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetSeerrLink(ctx, oas.GetSeerrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        "snatcharr_list_prowlarr_links",
		OperationID: "listProwlarrLinks",
		Scope:       domain.ScopeProwlarrRead,
		ReadOnly:    true,
		Description: "List Prowlarr links (API keys are never returned).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListProwlarrLinks(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_prowlarr_status",
		OperationID: "getProwlarrStatus",
		Scope:       domain.ScopeProwlarrRead,
		ReadOnly:    true,
		Description: "Live view of every enabled Prowlarr link (cached 30 s).",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetProwlarrStatus(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_prowlarr_link",
		OperationID: "getProwlarrLink",
		Scope:       domain.ScopeProwlarrRead,
		ReadOnly:    true,
		Description: "Get one Prowlarr link.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetProwlarrLink(ctx, oas.GetProwlarrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        "snatcharr_get_configarr_status",
		OperationID: "getConfigarrStatus",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Linked-mode state and the last import.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.GetConfigarrStatus(ctx)
		}),
	},
	{
		Name:        "snatcharr_list_notification_channels",
		OperationID: "listNotificationChannels",
		Scope:       domain.ScopeNotificationsRead,
		ReadOnly:    true,
		Description: "List all configured notification channels.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListNotificationChannels(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_notification_channel",
		OperationID: "getNotificationChannel",
		Scope:       domain.ScopeNotificationsRead,
		ReadOnly:    true,
		Description: "Get one notification channel.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetNotificationChannel(ctx, oas.GetNotificationChannelParams{ChannelId: id})
		}),
	},
	{
		Name:        "snatcharr_list_notification_rules",
		OperationID: "listNotificationRules",
		Scope:       domain.ScopeNotificationsRead,
		ReadOnly:    true,
		Description: "List all configured notification rules.",
		Register: registerNoParam(func(ctx context.Context, h *httpapi.Handlers) (any, error) {
			return h.ListNotificationRules(ctx)
		}),
	},
	{
		Name:        "snatcharr_get_notification_rule",
		OperationID: "getNotificationRule",
		Scope:       domain.ScopeNotificationsRead,
		ReadOnly:    true,
		Description: "Get one notification rule.",
		Register: registerIDParam(func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetNotificationRule(ctx, oas.GetNotificationRuleParams{RuleId: id})
		}),
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
		spec.Register(srv, h, user, tool)
	}
}
