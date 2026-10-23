// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
)

type omittedResourcePart struct {
	Part  string `json:"part"`
	Scope string `json:"scope"`
}

type instancesResourceSnapshot struct {
	Instances       any                   `json:"instances"`
	DownloadClients any                   `json:"download_clients,omitempty"`
	Prowlarr        any                   `json:"prowlarr,omitempty"`
	Omitted         []omittedResourcePart `json:"omitted,omitempty"`
}

type queueResourceSnapshot struct {
	InstanceID uuid.UUID `json:"instance_id"`
	Policy     any       `json:"policy"`
	Strikes    any       `json:"strikes"`
}

type historyResourceSnapshot struct {
	Runs   any `json:"runs"`
	Events any `json:"events"`
}

func requireCoreReadScope(auth requestAuth) error {
	if !slices.Contains(auth.scopes, domain.ScopeCoreRead) {
		// Second barrier behind the HTTP transport fence in checkToolCallFences:
		// refuse in-handler if requestAuth lacks core:read.
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: "key lacks required scope " + domain.ScopeCoreRead,
		}
	}
	return nil
}

func registerResources(srv *mcp.Server, h *httpapi.Handlers) {
	srv.AddResource(&mcp.Resource{
		URI:         "snatcharr://instances",
		Name:        "instances",
		Description: "Snapshot of configured *arr instances, download client status, and prowlarr status.",
		MIMEType:    "application/json",
	}, handleInstancesResource(h))

	srv.AddResource(&mcp.Resource{
		URI:         "snatcharr://history",
		Name:        "history",
		Description: "Recent execution runs and snatch history events.",
		MIMEType:    "application/json",
	}, handleHistoryResource(h))

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "snatcharr://queue/{instance_id}",
		Name:        "queue",
		Description: "Queue policy and active queue strikes for an instance.",
		MIMEType:    "application/json",
	}, handleQueueResource(h))
}

func handleInstancesResource(h *httpapi.Handlers) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if req.Params.URI != "snatcharr://instances" {
			return nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(req.Params.URI))
		}
		auth := requestAuthFrom(ctx)
		if err := requireCoreReadScope(auth); err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, auth.user), callTimeout)
		defer cancel()

		instances, err := callListInstances(callCtx, h)
		if err != nil {
			return nil, fmt.Errorf("llm: read instances resource: %w", err)
		}

		snap, err := buildInstancesSnapshot(callCtx, h, auth, instances)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(snap)
		if err != nil {
			return nil, fmt.Errorf("llm: marshal instances resource: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	}
}

func buildInstancesSnapshot(ctx context.Context, h *httpapi.Handlers, auth requestAuth, instances any) (instancesResourceSnapshot, error) {
	snap := instancesResourceSnapshot{
		Instances: instances,
	}
	if slices.Contains(auth.scopes, domain.ScopeDownloadClientsRead) {
		dlStatus, err := callGetDownloadClientStatus(ctx, h)
		if err != nil {
			return snap, fmt.Errorf("llm: read download clients status: %w", err)
		}
		snap.DownloadClients = dlStatus
	} else {
		snap.Omitted = append(snap.Omitted, omittedResourcePart{
			Part:  "download_clients",
			Scope: domain.ScopeDownloadClientsRead,
		})
	}
	if slices.Contains(auth.scopes, domain.ScopeProwlarrRead) {
		prowlarrStatus, err := callGetProwlarrStatus(ctx, h)
		if err != nil {
			return snap, fmt.Errorf("llm: read prowlarr status: %w", err)
		}
		snap.Prowlarr = prowlarrStatus
	} else {
		snap.Omitted = append(snap.Omitted, omittedResourcePart{
			Part:  "prowlarr",
			Scope: domain.ScopeProwlarrRead,
		})
	}
	return snap, nil
}

func parseQueueResourceURI(uri string) (uuid.UUID, error) {
	if !strings.HasPrefix(uri, "snatcharr://queue/") {
		return uuid.Nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(uri))
	}
	rawID := strings.Trim(strings.TrimPrefix(uri, "snatcharr://queue/"), "/")
	if rawID == "" {
		return uuid.Nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(uri))
	}
	parsedID, err := uuid.Parse(rawID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(uri))
	}
	return parsedID, nil
}

func fetchQueueSnapshot(ctx context.Context, h *httpapi.Handlers, uri string, instID uuid.UUID) (*queueResourceSnapshot, error) {
	policy, err := callGetQueuePolicy(ctx, h, instID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(uri))
		}
		return nil, fmt.Errorf("llm: read queue policy: %w", err)
	}
	strikes, err := callListQueueStrikes(ctx, h, instID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(uri))
		}
		return nil, fmt.Errorf("llm: read queue strikes: %w", err)
	}
	return &queueResourceSnapshot{
		InstanceID: instID,
		Policy:     policy,
		Strikes:    strikes,
	}, nil
}

func handleQueueResource(h *httpapi.Handlers) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		instanceID, err := parseQueueResourceURI(uri)
		if err != nil {
			return nil, err
		}
		auth := requestAuthFrom(ctx)
		if err = requireCoreReadScope(auth); err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, auth.user), callTimeout)
		defer cancel()

		snap, err := fetchQueueSnapshot(callCtx, h, uri, instanceID)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(snap)
		if err != nil {
			return nil, fmt.Errorf("llm: marshal queue resource: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      uri,
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	}
}

func handleHistoryResource(h *httpapi.Handlers) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if req.Params.URI != "snatcharr://history" {
			return nil, fmt.Errorf("%w", mcp.ResourceNotFoundError(req.Params.URI))
		}
		auth := requestAuthFrom(ctx)
		if err := requireCoreReadScope(auth); err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(httpapi.WithUser(ctx, auth.user), callTimeout)
		defer cancel()

		runs, err := callListRuns(callCtx, h, listRunsInput{})
		if err != nil {
			return nil, fmt.Errorf("llm: read runs history: %w", err)
		}
		pageSize := 100
		events, err := callListEvents(callCtx, h, listEventsInput{PageSize: &pageSize})
		if err != nil {
			return nil, fmt.Errorf("llm: read events history: %w", err)
		}
		data, err := json.Marshal(historyResourceSnapshot{
			Runs:   runs,
			Events: events,
		})
		if err != nil {
			return nil, fmt.Errorf("llm: marshal history resource: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	}
}
