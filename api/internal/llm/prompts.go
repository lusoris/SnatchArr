// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const whyNotSnatchedInstructions = "To diagnose why the media item was not snatched on the specified instance, call the following read tools in order:\n" +
	"1. snatcharr_get_instance: verify the instance has enabled=true and inspect last_check_at, last_error, and last_seen_version.\n" +
	"2. snatcharr_get_policy: inspect the snatch policy for missing_per_cycle, upgrade_per_cycle, hourly_cap, monitored_only, skip_future_releases, skip_unavailable_movies, release_delay_h, processed_ttl_h, afterglow_max_h, dry_run, include_tags/exclude_tags, include_quality_profiles/exclude_quality_profiles, and the app-specific missing/upgrade modes.\n" +
	"3. snatcharr_get_queue_policy: inspect the queue policy for min_custom_format_score and active queue rules.\n" +
	"4. snatcharr_get_hourly_caps: check used, cap, and resets_at for the instance and global scopes to verify stamina is not exhausted.\n" +
	"5. snatcharr_list_queue_strikes: inspect download_id, queue_id, rule, count, threshold, last_seen, imported_at, and import_refused to see whether a download is struck or stalled.\n" +
	"6. snatcharr_list_events: inspect recent snatch events (ts, level, type, entity_type, entity_id, title, detail) for the item.\n" +
	"7. snatcharr_list_runs: inspect recent snatch execution runs for the instance."

const dailySummaryInstructions = "To synthesize 24-hour activity across all instances, call the following read tools in order:\n" +
	"1. snatcharr_list_instances: list all configured instances (id, kind, name, base_url, enabled, source, last_seen_version, last_check_at, last_error) to establish the monitoring inventory.\n" +
	"2. snatcharr_get_hourly_caps: check current stamina usage (scope, instance_id, used, cap, resets_at) across instances and global scope.\n" +
	"3. snatcharr_list_runs: fetch recent runs (filtered by instance_id and limit); keep those from the past 24 hours by their timestamps.\n" +
	"4. snatcharr_list_events: retrieve snatch events (ts, level, type, entity_type, entity_id, title, detail) for completed dispatches and warnings/errors.\n" +
	"5. snatcharr_get_download_client_status: inspect download client reachability, active/queued counts, and transfer rates.\n" +
	"6. snatcharr_get_prowlarr_status: check Prowlarr link reachability and per-indexer limits (query_limit, grab_limit, limits_unit, used_queries, used_grabs, disabled_till, exhausted, exhausted_by)."

const capHeadroomInstructions = "To summarize remaining search stamina, limits, and headroom, call the following read tools in order:\n" +
	"1. snatcharr_get_hourly_caps: inspect rolling 60-minute stamina (scope, instance_id, used, cap, resets_at) for instance and global scopes.\n" +
	"2. snatcharr_get_policy: retrieve each instance's SnatchPolicy to check configured hourly_cap and max_queue_size backpressure threshold (list instances with snatcharr_list_instances if instance IDs are not yet known).\n" +
	"3. snatcharr_get_prowlarr_status: inspect per-indexer headroom across Prowlarr links (query_limit, grab_limit, limits_unit, used_queries, used_grabs, disabled_till, exhausted, exhausted_by).\n" +
	"4. snatcharr_list_schedules: check active schedule windows (action pause or cap_override, cap_value, days, start, end, tz).\n" +
	"5. snatcharr_get_queue_policy: optionally inspect queue governance settings and min_custom_format_score alongside SnatchPolicy.max_queue_size."

type whyNotSnatchedArgs struct {
	InstanceID string `json:"instance_id"`
	Item       string `json:"item"`
}

func registerPrompts(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "why_not_snatched",
		Description: "Diagnose why a specific media item was not snatched on an instance.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "instance_id",
				Description: "Instance UUID to diagnose",
				Required:    true,
			},
			{
				Name:        "item",
				Description: "Media item title, release name, or identifier",
				Required:    true,
			},
		},
	}, handleWhyNotSnatched)

	srv.AddPrompt(&mcp.Prompt{
		Name:        "daily_summary",
		Description: "Synthesizes 24-hour activity across all instances.",
	}, handleDailySummary)

	srv.AddPrompt(&mcp.Prompt{
		Name:        "cap_headroom",
		Description: "Summarizes remaining search stamina per indexer, current hourly limits, reset windows, and queue backpressure thresholds.",
	}, handleCapHeadroom)
}

func handleWhyNotSnatched(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	auth := requestAuthFrom(ctx)
	if err := requireCoreReadScope(auth); err != nil {
		return nil, err
	}
	rawInstID := ""
	rawItem := ""
	if req.Params != nil && req.Params.Arguments != nil {
		rawInstID = req.Params.Arguments["instance_id"]
		rawItem = req.Params.Arguments["item"]
	}
	if rawInstID == "" {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: "missing required argument instance_id",
		}
	}
	parsedUUID, err := uuid.Parse(rawInstID)
	if err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: fmt.Sprintf("invalid instance_id %q: must be a valid UUID", rawInstID),
		}
	}
	if rawItem == "" {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: "missing required argument item",
		}
	}
	argsJSON, err := json.Marshal(whyNotSnatchedArgs{
		InstanceID: parsedUUID.String(),
		Item:       rawItem,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: marshal prompt arguments: %w", err)
	}
	return &mcp.GetPromptResult{
		Description: "Diagnose why a specific media item was not snatched on an instance.",
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: whyNotSnatchedInstructions,
				},
			},
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: "arguments: " + string(argsJSON),
				},
			},
		},
	}, nil
}

func handleDailySummary(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	auth := requestAuthFrom(ctx)
	if err := requireCoreReadScope(auth); err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: "Synthesizes 24-hour activity across all instances.",
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: dailySummaryInstructions,
				},
			},
		},
	}, nil
}

func handleCapHeadroom(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	auth := requestAuthFrom(ctx)
	if err := requireCoreReadScope(auth); err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: "Summarizes remaining search stamina per indexer, current hourly limits, reset windows, and queue backpressure thresholds.",
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: capHeadroomInstructions,
				},
			},
		},
	}, nil
}
