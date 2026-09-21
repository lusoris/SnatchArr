// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net/http"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func httpStatusText(status int) string {
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "Error"
}

func requireAdmin(ctx context.Context) error {
	u, ok := UserFrom(ctx)
	if !ok {
		return domain.ErrUnauthorized
	}
	if u.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	return nil
}

// ListInstances returns every instance without secrets.
func (h *Handlers) ListInstances(ctx context.Context) ([]oas.Instance, error) {
	list, err := h.instances.List(ctx)
	if err != nil {
		return nil, err
	}
	return instancesToOAS(list), nil
}

// GetInstance returns one instance.
func (h *Handlers) GetInstance(ctx context.Context, params oas.GetInstanceParams) (*oas.Instance, error) {
	inst, err := h.instances.Get(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	out := instanceToOAS(inst)
	return &out, nil
}

// CreateInstance adds a manual instance.
func (h *Handlers) CreateInstance(ctx context.Context, req *oas.InstanceInput) (*oas.Instance, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	inst, err := h.instances.Create(ctx, inputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := instanceToOAS(inst)
	return &out, nil
}

// UpdateInstance edits a manual instance.
func (h *Handlers) UpdateInstance(ctx context.Context, req *oas.InstanceUpdate, params oas.UpdateInstanceParams) (*oas.Instance, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	inst, err := h.instances.Update(ctx, params.InstanceId, updateFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := instanceToOAS(inst)
	return &out, nil
}

// DeleteInstance removes an instance.
func (h *Handlers) DeleteInstance(ctx context.Context, params oas.DeleteInstanceParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.instances.Delete(ctx, params.InstanceId)
}

// TestInstance probes a stored instance.
func (h *Handlers) TestInstance(ctx context.Context, params oas.TestInstanceParams) (*oas.ProbeResult, error) {
	res, err := h.instances.Test(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: res.AppName, Version: res.Version}, nil
}

// TestInstanceInput probes unsaved credentials.
func (h *Handlers) TestInstanceInput(ctx context.Context, req *oas.InstanceInput) (*oas.ProbeResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	res, err := h.instances.TestInput(ctx, inputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: res.AppName, Version: res.Version}, nil
}

// GetPolicy returns an instance's snatch policy.
func (h *Handlers) GetPolicy(ctx context.Context, params oas.GetPolicyParams) (*oas.SnatchPolicy, error) {
	p, err := h.policies.Get(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	return policyToOAS(p), nil
}

// UpdatePolicy replaces an instance's snatch policy.
func (h *Handlers) UpdatePolicy(ctx context.Context, req *oas.SnatchPolicy, params oas.UpdatePolicyParams) (*oas.SnatchPolicy, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := h.policies.Update(ctx, policyFromOAS(params.InstanceId, req))
	if err != nil {
		return nil, err
	}
	return policyToOAS(p), nil
}
