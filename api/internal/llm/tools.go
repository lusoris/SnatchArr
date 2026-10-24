// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
)

const callTimeout = 30 * time.Second

func parseUUIDParam(in map[string]json.RawMessage, paramName string) (uuid.UUID, error) {
	raw, ok := in[paramName]
	if !ok || len(raw) == 0 {
		return uuid.Nil, fmt.Errorf("%w: missing %s", domain.ErrBadRequest, paramName)
	}
	var val string
	if err := json.Unmarshal(raw, &val); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode %s: %w", domain.ErrBadRequest, paramName, err)
	}
	if val == "" {
		return uuid.Nil, fmt.Errorf("%w: missing %s", domain.ErrBadRequest, paramName)
	}
	parsed, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s %q: %w", domain.ErrBadRequest, paramName, val, err)
	}
	return parsed, nil
}

func parseStringParam(in map[string]json.RawMessage, paramName string) (string, error) {
	raw, ok := in[paramName]
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("%w: missing %s", domain.ErrBadRequest, paramName)
	}
	var val string
	if err := json.Unmarshal(raw, &val); err != nil {
		return "", fmt.Errorf("%w: decode %s: %w", domain.ErrBadRequest, paramName, err)
	}
	return val, nil
}

func parsePositiveIntParam(in map[string]json.RawMessage, paramName string) (int, error) {
	raw, ok := in[paramName]
	if !ok || len(raw) == 0 {
		return 0, fmt.Errorf("%w: missing %s", domain.ErrBadRequest, paramName)
	}
	var val int
	if err := json.Unmarshal(raw, &val); err != nil {
		return 0, fmt.Errorf("%w: decode %s: %w", domain.ErrBadRequest, paramName, err)
	}
	if val < 1 {
		return 0, fmt.Errorf("%w: %s must be at least 1", domain.ErrBadRequest, paramName)
	}
	return val, nil
}

func parseConfirmParam(in map[string]json.RawMessage) (bool, error) {
	raw, ok := in["confirm"]
	if !ok || len(raw) == 0 {
		return false, nil
	}
	var val bool
	if err := json.Unmarshal(raw, &val); err != nil {
		return false, fmt.Errorf("%w: decode confirm: %w", domain.ErrBadRequest, err)
	}
	return val, nil
}

type bodyInput struct {
	Body json.RawMessage `json:"body"`
}

type confirmBodyInput struct {
	Confirm bool            `json:"confirm,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

type listRunsInput struct {
	InstanceID *string `json:"instance_id,omitempty"`
	Limit      *int    `json:"limit,omitempty"`
	Offset     *int    `json:"offset,omitempty"`
}

type listEventsInput struct {
	InstanceID *string `json:"instance_id,omitempty"`
	BeforeID   *int64  `json:"before_id,omitempty"`
	Type       *string `json:"type,omitempty"`
	PageSize   *int    `json:"page_size,omitempty"`
}

type listLogsInput struct {
	Level  *string `json:"level,omitempty"`
	Q      *string `json:"q,omitempty"`
	Source *string `json:"source,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
}

type registerContext struct {
	srv    *mcp.Server
	h      *httpapi.Handlers
	logger *slog.Logger
	user   domain.User
	keyID  string
}

type toolSpec struct {
	Name        string
	OperationID string
	Scope       string
	Module      string
	ReadOnly    bool
	Destructive bool
	Description string
	Register    func(rc registerContext, tool *mcp.Tool)
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

func registerParamTool[In any](convertAndCall func(ctx context.Context, h *httpapi.Handlers, in In) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[In, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			out, err := convertAndCall(callCtx, rc.h, in)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerNoParam(call func(ctx context.Context, h *httpapi.Handlers) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return registerParamTool(func(ctx context.Context, h *httpapi.Handlers, _ struct{}) (any, error) {
		return call(ctx, h)
	})
}

func registerIDParam(paramName string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return registerParamTool(func(ctx context.Context, h *httpapi.Handlers, in map[string]json.RawMessage) (any, error) {
		parsed, err := parseUUIDParam(in, paramName)
		if err != nil {
			return nil, err
		}
		return call(ctx, h, parsed)
	})
}

// oasBody is the method every generated request body carries. It is
// checked at run time, not as a type-set constraint: semgrep cannot parse a
// `*T` constraint element.
type oasBody interface {
	json.Unmarshaler
}

type oasValidator interface {
	Validate() error
}

func decodeOASBody[T any](raw json.RawMessage) (*T, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: request body is required", domain.ErrBadRequest)
	}
	var body T
	ptr, ok := any(&body).(oasBody)
	if !ok {
		return nil, fmt.Errorf("llm: body type %T lacks UnmarshalJSON", &body)
	}
	if err := ptr.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("%w: decode body: %w", domain.ErrBadRequest, err)
	}
	if v, ok := any(ptr).(oasValidator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("%w: validate body: %w", domain.ErrBadRequest, err)
		}
	}
	return &body, nil
}

func decodeOptionalOASBody[T any](raw json.RawMessage) (*T, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil //nolint:nilnil // nil body signals absent optional body
	}
	return decodeOASBody[T](raw)
}

func logWriteToolCall(ctx context.Context, rc registerContext, name string) {
	rc.logger.InfoContext(ctx, "llm: write tool called",
		slog.String("tool", name),
		slog.String("owner", rc.user.ID.String()),
		slog.String("key_id", rc.keyID),
	)
}

func logUnconfirmedDestructive(ctx context.Context, rc registerContext, name string) {
	rc.logger.WarnContext(ctx, "llm: destructive tool unconfirmed",
		slog.String("tool", name),
		slog.String("owner", rc.user.ID.String()),
		slog.String("key_id", rc.keyID),
	)
}

func registerWriteIDBody[Body any](paramName string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *Body) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[map[string]json.RawMessage, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]json.RawMessage) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			parsedID, err := parseUUIDParam(in, paramName)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			body, err := decodeOASBody[Body](in["body"])
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, parsedID, body)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerWriteBody[Body any](call func(ctx context.Context, h *httpapi.Handlers, body *Body) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[bodyInput, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in bodyInput) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			body, err := decodeOASBody[Body](in.Body)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, body)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerWriteOptionalBody[Body any](call func(ctx context.Context, h *httpapi.Handlers, body *Body) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[bodyInput, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in bodyInput) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			body, err := decodeOptionalOASBody[Body](in.Body)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, body)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerDestructiveOptionalBody[Body any](call func(ctx context.Context, h *httpapi.Handlers, body *Body) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[confirmBodyInput, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in confirmBodyInput) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			if !in.Confirm {
				logUnconfirmedDestructive(callCtx, rc, tool.Name)
				return toolError("confirmation required: pass confirm=true")
			}
			logWriteToolCall(callCtx, rc, tool.Name)
			body, err := decodeOptionalOASBody[Body](in.Body)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, body)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerWriteID(paramName string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[map[string]json.RawMessage, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]json.RawMessage) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			parsedID, err := parseUUIDParam(in, paramName)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, parsedID)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerWriteIDString(idParam, strParam string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, val string) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[map[string]json.RawMessage, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]json.RawMessage) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			parsedID, err := parseUUIDParam(in, idParam)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			val, err := parseStringParam(in, strParam)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, parsedID, val)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerWriteIDInt(idParam, intParam string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, val int) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[map[string]json.RawMessage, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]json.RawMessage) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			logWriteToolCall(callCtx, rc, tool.Name)
			parsedID, err := parseUUIDParam(in, idParam)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			val, err := parsePositiveIntParam(in, intParam)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, parsedID, val)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
}

func registerDestructiveID(paramName string, call func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error)) func(rc registerContext, tool *mcp.Tool) {
	return func(rc registerContext, tool *mcp.Tool) {
		mcp.AddTool[map[string]json.RawMessage, any](rc.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]json.RawMessage) (*mcp.CallToolResult, any, error) {
			callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, rc.user), callTimeout)
			defer cancel()
			confirmed, err := parseConfirmParam(in)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			if !confirmed {
				logUnconfirmedDestructive(callCtx, rc, tool.Name)
				return toolError("confirmation required: pass confirm=true")
			}
			logWriteToolCall(callCtx, rc, tool.Name)
			parsedID, err := parseUUIDParam(in, paramName)
			if err != nil {
				return handleResult(callCtx, rc.h, nil, err)
			}
			out, err := call(callCtx, rc.h, parsedID)
			return handleResult(callCtx, rc.h, out, err)
		})
	}
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

func callListInstances(ctx context.Context, h *httpapi.Handlers) (any, error) {
	return h.ListInstances(ctx)
}

func callGetDownloadClientStatus(ctx context.Context, h *httpapi.Handlers) (any, error) {
	return h.GetDownloadClientStatus(ctx)
}

func callGetProwlarrStatus(ctx context.Context, h *httpapi.Handlers) (any, error) {
	return h.GetProwlarrStatus(ctx)
}

func callGetQueuePolicy(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
	return h.GetQueuePolicy(ctx, oas.GetQueuePolicyParams{InstanceId: id})
}

func callListQueueStrikes(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
	return h.ListQueueStrikes(ctx, oas.ListQueueStrikesParams{InstanceId: id})
}

func callListQueueItems(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (*oas.QueueInspection, error) {
	return h.ListQueueItems(ctx, oas.ListQueueItemsParams{InstanceId: id})
}

func callListRuns(ctx context.Context, h *httpapi.Handlers, in listRunsInput) (any, error) {
	params, err := convertListRunsParams(in)
	if err != nil {
		return nil, err
	}
	return h.ListRuns(ctx, params)
}

func callListEvents(ctx context.Context, h *httpapi.Handlers, in listEventsInput) (any, error) {
	params, err := convertListEventsParams(in)
	if err != nil {
		return nil, err
	}
	return h.ListEvents(ctx, params)
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
		Register:    registerNoParam(callListInstances),
	},
	{
		Name:        "snatcharr_get_instance",
		OperationID: "getInstance",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Get one instance.",
		Register: registerIDParam("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_policy",
		OperationID: "getPolicy",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Snatch policy of an instance.",
		Register: registerIDParam("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetPolicy(ctx, oas.GetPolicyParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_policy_filter_options",
		OperationID: "getPolicyFilterOptions",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Tags and quality profiles the instance offers for the policy filters.",
		Register: registerIDParam("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetPolicyFilterOptions(ctx, oas.GetPolicyFilterOptionsParams{InstanceId: id})
		}),
	},
	{
		Name:        "snatcharr_get_queue_policy",
		OperationID: "getQueuePolicy",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Queue management policy of an instance.",
		Register:    registerIDParam("instanceId", callGetQueuePolicy),
	},
	{
		Name:        "snatcharr_list_queue_strikes",
		OperationID: "listQueueStrikes",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Active queue strikes of an instance.",
		Register:    registerIDParam("instanceId", callListQueueStrikes),
	},
	{
		Name:        "snatcharr_list_runs",
		OperationID: "listRuns",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Recent snatch runs, newest first.",
		Register:    registerParamTool(callListRuns),
	},
	{
		Name:        "snatcharr_get_run",
		OperationID: "getRun",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Load a single snatch run by ID.",
		Register: registerIDParam("runId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetRun(ctx, oas.GetRunParams{RunId: id})
		}),
	},
	{
		Name:        "snatcharr_list_events",
		OperationID: "listEvents",
		Scope:       domain.ScopeCoreRead,
		ReadOnly:    true,
		Description: "Snatch history, newest first (cursor on before_id).",
		Register:    registerParamTool(callListEvents),
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
		Register:    registerNoParam(callGetDownloadClientStatus),
	},
	{
		Name:        "snatcharr_get_download_client",
		OperationID: "getDownloadClient",
		Scope:       domain.ScopeDownloadClientsRead,
		ReadOnly:    true,
		Description: "Get one download client.",
		Register: registerIDParam("clientId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
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
		Register: registerIDParam("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
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
		Register:    registerNoParam(callGetProwlarrStatus),
	},
	{
		Name:        "snatcharr_get_prowlarr_link",
		OperationID: "getProwlarrLink",
		Scope:       domain.ScopeProwlarrRead,
		ReadOnly:    true,
		Description: "Get one Prowlarr link.",
		Register: registerIDParam("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
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
		Register: registerIDParam("channelId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
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
		Register: registerIDParam("ruleId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.GetNotificationRule(ctx, oas.GetNotificationRuleParams{RuleId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTriggerRun,
		OperationID: "triggerRun",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Quickie - queue a snatch now (ignores the refractory period).",
		Register: registerWriteIDBody[oas.RunTrigger]("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.RunTrigger) (any, error) {
			return h.TriggerRun(ctx, body, oas.TriggerRunParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdatePolicy,
		OperationID: "updatePolicy",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Replace the snatch policy of an instance.",
		Register: registerWriteIDBody[oas.SnatchPolicy]("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.SnatchPolicy) (any, error) {
			return h.UpdatePolicy(ctx, body, oas.UpdatePolicyParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCancelRun,
		OperationID: "cancelRun",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Cancel a queued or leased run.",
		Register: registerDestructiveID("runId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.CancelRun(ctx, oas.CancelRunParams{RunId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteInstance,
		OperationID: "deleteInstance",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete an instance and everything it owns.",
		Register: registerDestructiveID("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.DeleteInstance(ctx, oas.DeleteInstanceParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateInstance,
		OperationID: "createInstance",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Add a manual instance.",
		Register: registerWriteBody[oas.InstanceInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.InstanceInput) (any, error) {
			return h.CreateInstance(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolTestInstanceInput,
		OperationID: "testInstanceInput",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe credentials that are not stored yet.",
		Register: registerWriteBody[oas.InstanceInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.InstanceInput) (any, error) {
			return h.TestInstanceInput(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateInstance,
		OperationID: "updateInstance",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a manual instance (omit api_key to keep it).",
		Register: registerWriteIDBody[oas.InstanceUpdate]("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.InstanceUpdate) (any, error) {
			return h.UpdateInstance(ctx, body, oas.UpdateInstanceParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTestInstance,
		OperationID: "testInstance",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe a stored instance and record the outcome.",
		Register: registerWriteID("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.TestInstance(ctx, oas.TestInstanceParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateQueuePolicy,
		OperationID: "updateQueuePolicy",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Replace the queue management policy of an instance.",
		Register: registerWriteIDBody[oas.QueuePolicy]("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.QueuePolicy) (any, error) {
			return h.UpdateQueuePolicy(ctx, body, oas.UpdateQueuePolicyParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolPreviewRun,
		OperationID: "previewRun",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Start a dry-run / preview snatch run.",
		Register: registerWriteIDString("instanceId", "kind", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, kind string) (any, error) {
			return h.PreviewRun(ctx, oas.PreviewRunParams{InstanceId: id, Kind: kind})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteInstanceEvents,
		OperationID: "deleteInstanceEvents",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Clear the history of one instance.",
		Register: registerDestructiveID("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteInstanceEvents(ctx, oas.DeleteInstanceEventsParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolResetState,
		OperationID: "resetState",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Forget processed items (one instance or all).",
		Register: registerDestructiveOptionalBody[oas.StateReset](func(ctx context.Context, h *httpapi.Handlers, body *oas.StateReset) (any, error) {
			var opt oas.OptStateReset
			if body != nil {
				opt = oas.NewOptStateReset(*body)
			}
			return h.ResetState(ctx, opt)
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateSchedule,
		OperationID: "createSchedule",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Add a schedule window.",
		Register: registerWriteBody[oas.ScheduleInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.ScheduleInput) (any, error) {
			return h.CreateSchedule(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateSchedule,
		OperationID: "updateSchedule",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Replace a schedule window.",
		Register: registerWriteIDBody[oas.ScheduleInput]("scheduleId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.ScheduleInput) (any, error) {
			return h.UpdateSchedule(ctx, body, oas.UpdateScheduleParams{ScheduleId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteSchedule,
		OperationID: "deleteSchedule",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a schedule window.",
		Register: registerDestructiveID("scheduleId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteSchedule(ctx, oas.DeleteScheduleParams{ScheduleId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolImportConfigarr,
		OperationID: "importConfigarr",
		Scope:       domain.ScopeCoreWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Import instances from a Configarr config.yml (defaults to the linked files).",
		Register: registerWriteOptionalBody[oas.ConfigarrImportRequest](func(ctx context.Context, h *httpapi.Handlers, body *oas.ConfigarrImportRequest) (any, error) {
			var opt oas.OptConfigarrImportRequest
			if body != nil {
				opt = oas.NewOptConfigarrImportRequest(*body)
			}
			return h.ImportConfigarr(ctx, opt)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateSettings,
		OperationID: "updateSettings",
		Scope:       domain.ScopeAdminWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Replace the runtime settings.",
		Register: registerWriteBody[oas.Settings](func(ctx context.Context, h *httpapi.Handlers, body *oas.Settings) (any, error) {
			current, err := h.GetSettings(ctx)
			if err != nil {
				return nil, err
			}
			// Security fence: tool never changes llm_enabled or llm_write_tools.
			body.LlmEnabled = current.LlmEnabled
			body.LlmWriteTools = current.LlmWriteTools
			return h.UpdateSettings(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateDownloadClient,
		OperationID: "createDownloadClient",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Add a download client by hand.",
		Register: registerWriteBody[oas.DownloadClientInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.DownloadClientInput) (any, error) {
			return h.CreateDownloadClient(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolTestDownloadClientInput,
		OperationID: "testDownloadClientInput",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Observe a download client whose credentials are not stored yet.",
		Register: registerWriteBody[oas.DownloadClientInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.DownloadClientInput) (any, error) {
			return h.TestDownloadClientInput(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateDownloadClient,
		OperationID: "updateDownloadClient",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a download client (omit secret to keep it).",
		Register: registerWriteIDBody[oas.DownloadClientInput]("clientId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.DownloadClientInput) (any, error) {
			return h.UpdateDownloadClient(ctx, body, oas.UpdateDownloadClientParams{ClientId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteDownloadClient,
		OperationID: "deleteDownloadClient",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a download client.",
		Register: registerDestructiveID("clientId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteDownloadClient(ctx, oas.DeleteDownloadClientParams{ClientId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTestDownloadClient,
		OperationID: "testDownloadClient",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Observe a stored download client now and record the outcome.",
		Register: registerWriteID("clientId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.TestDownloadClient(ctx, oas.TestDownloadClientParams{ClientId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDiscoverDownloadClients,
		OperationID: "discoverDownloadClients",
		Scope:       domain.ScopeDownloadClientsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Import the download clients this instance has configured.",
		Register: registerWriteID("instanceId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.DiscoverDownloadClients(ctx, oas.DiscoverDownloadClientsParams{InstanceId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateSeerrLink,
		OperationID: "createSeerrLink",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Add a Seerr link.",
		Register: registerWriteBody[oas.SeerrLinkInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.SeerrLinkInput) (any, error) {
			return h.CreateSeerrLink(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolTestSeerrLinkInput,
		OperationID: "testSeerrLinkInput",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe Seerr credentials that are not stored yet.",
		Register: registerWriteBody[oas.SeerrLinkInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.SeerrLinkInput) (any, error) {
			return h.TestSeerrLinkInput(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolSnatchSeerrRequest,
		OperationID: "snatchSeerrRequest",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Quickie focused on one request (missing snatch of its instance, limited to that movie or series).",
		Register: registerWriteIDInt("linkId", "requestId", func(ctx context.Context, h *httpapi.Handlers, linkID uuid.UUID, reqID int) (any, error) {
			return h.SnatchSeerrRequest(ctx, oas.SnatchSeerrRequestParams{LinkId: linkID, RequestId: reqID})
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateSeerrLink,
		OperationID: "updateSeerrLink",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a Seerr link (omit api_key to keep it).",
		Register: registerWriteIDBody[oas.SeerrLinkInput]("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.SeerrLinkInput) (any, error) {
			return h.UpdateSeerrLink(ctx, body, oas.UpdateSeerrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteSeerrLink,
		OperationID: "deleteSeerrLink",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a Seerr link and its cached requests.",
		Register: registerDestructiveID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteSeerrLink(ctx, oas.DeleteSeerrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTestSeerrLink,
		OperationID: "testSeerrLink",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe a stored Seerr link and record the outcome.",
		Register: registerWriteID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.TestSeerrLink(ctx, oas.TestSeerrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolSyncSeerrLink,
		OperationID: "syncSeerrLink",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Refresh the request cache now (also runs every five minutes).",
		Register: registerWriteID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.SyncSeerrLink(ctx, oas.SyncSeerrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolImportSeerrInstances,
		OperationID: "importSeerrInstances",
		Scope:       domain.ScopeRequestsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Create instances from the Sonarr/Radarr servers configured in Seerr.",
		Register: registerWriteID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.ImportSeerrInstances(ctx, oas.ImportSeerrInstancesParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateProwlarrLink,
		OperationID: "createProwlarrLink",
		Scope:       domain.ScopeProwlarrWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Add a Prowlarr link.",
		Register: registerWriteBody[oas.ProwlarrLinkInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.ProwlarrLinkInput) (any, error) {
			return h.CreateProwlarrLink(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolTestProwlarrLinkInput,
		OperationID: "testProwlarrLinkInput",
		Scope:       domain.ScopeProwlarrWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe Prowlarr credentials that are not stored yet.",
		Register: registerWriteBody[oas.ProwlarrLinkInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.ProwlarrLinkInput) (any, error) {
			return h.TestProwlarrLinkInput(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateProwlarrLink,
		OperationID: "updateProwlarrLink",
		Scope:       domain.ScopeProwlarrWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a Prowlarr link (omit api_key to keep it).",
		Register: registerWriteIDBody[oas.ProwlarrLinkInput]("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.ProwlarrLinkInput) (any, error) {
			return h.UpdateProwlarrLink(ctx, body, oas.UpdateProwlarrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteProwlarrLink,
		OperationID: "deleteProwlarrLink",
		Scope:       domain.ScopeProwlarrWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a Prowlarr link.",
		Register: registerDestructiveID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteProwlarrLink(ctx, oas.DeleteProwlarrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTestProwlarrLink,
		OperationID: "testProwlarrLink",
		Scope:       domain.ScopeProwlarrWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Probe a stored Prowlarr link and record the outcome.",
		Register: registerWriteID("linkId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return h.TestProwlarrLink(ctx, oas.TestProwlarrLinkParams{LinkId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateNotificationChannel,
		OperationID: "createNotificationChannel",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Create a notification channel.",
		Register: registerWriteBody[oas.NotificationChannelInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.NotificationChannelInput) (any, error) {
			return h.CreateNotificationChannel(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateNotificationChannel,
		OperationID: "updateNotificationChannel",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a notification channel.",
		Register: registerWriteIDBody[oas.NotificationChannelUpdate]("channelId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.NotificationChannelUpdate) (any, error) {
			return h.UpdateNotificationChannel(ctx, body, oas.UpdateNotificationChannelParams{ChannelId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteNotificationChannel,
		OperationID: "deleteNotificationChannel",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a notification channel.",
		Register: registerDestructiveID("channelId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteNotificationChannel(ctx, oas.DeleteNotificationChannelParams{ChannelId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolTestNotificationChannel,
		OperationID: "testNotificationChannel",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Send a synchronous test notification to a channel.",
		Register: registerWriteID("channelId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.TestNotificationChannel(ctx, oas.TestNotificationChannelParams{ChannelId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolCreateNotificationRule,
		OperationID: "createNotificationRule",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Create a notification rule.",
		Register: registerWriteBody[oas.NotificationRuleInput](func(ctx context.Context, h *httpapi.Handlers, body *oas.NotificationRuleInput) (any, error) {
			return h.CreateNotificationRule(ctx, body)
		}),
	},
	{
		Name:        domain.LLMWriteToolUpdateNotificationRule,
		OperationID: "updateNotificationRule",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: false,
		Description: "Update a notification rule.",
		Register: registerWriteIDBody[oas.NotificationRuleUpdate]("ruleId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID, body *oas.NotificationRuleUpdate) (any, error) {
			return h.UpdateNotificationRule(ctx, body, oas.UpdateNotificationRuleParams{RuleId: id})
		}),
	},
	{
		Name:        domain.LLMWriteToolDeleteNotificationRule,
		OperationID: "deleteNotificationRule",
		Scope:       domain.ScopeNotificationsWrite,
		ReadOnly:    false,
		Destructive: true,
		Description: "Delete a notification rule.",
		Register: registerDestructiveID("ruleId", func(ctx context.Context, h *httpapi.Handlers, id uuid.UUID) (any, error) {
			return struct{}{}, h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: id})
		}),
	},
}

func lookupTool(specs []toolSpec, name string) (toolSpec, bool) {
	for _, spec := range specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return toolSpec{}, false
}

func registerTools(rc registerContext, specs []toolSpec, schemas map[string]json.RawMessage, scopes, writeTools, disabledModules []string) {
	for _, spec := range specs {
		if spec.Module != "" && slices.Contains(disabledModules, spec.Module) {
			continue
		}
		if !slices.Contains(scopes, spec.Scope) {
			continue
		}
		if !spec.ReadOnly && !slices.Contains(writeTools, spec.Name) {
			continue
		}
		destructive := spec.Destructive
		tool := &mcp.Tool{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: schemas[spec.Name],
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    spec.ReadOnly,
				DestructiveHint: &destructive,
			},
		}
		spec.Register(rc, tool)
	}
}
