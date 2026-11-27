// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"
	"slices"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

func (h *Handlers) requireLibraryProcessing() (*libraryprocessing.Service, error) {
	if h.libraryProcessing == nil {
		return nil, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleLibraryProcessing)
	}
	return h.libraryProcessing, nil
}

// ListLibraryProcessingRules returns all stored library processing rules in precedence order.
func (h *Handlers) ListLibraryProcessingRules(ctx context.Context) ([]oas.LibraryProcessingRule, error) {
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	rows, err := svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.LibraryProcessingRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, storedLibraryProcessingRuleToOAS(r))
	}
	return out, nil
}

// GetLibraryProcessingRule returns one stored library processing rule by ID.
func (h *Handlers) GetLibraryProcessingRule(
	ctx context.Context,
	params oas.GetLibraryProcessingRuleParams,
) (*oas.LibraryProcessingRule, error) {
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	row, err := svc.Get(ctx, params.RuleId)
	if err != nil {
		return nil, err
	}
	out := storedLibraryProcessingRuleToOAS(row)
	return &out, nil
}

// CreateLibraryProcessingRule validates and persists a scoped library processing rule (admin only).
func (h *Handlers) CreateLibraryProcessingRule(
	ctx context.Context,
	req *oas.LibraryProcessingRuleInput,
) (*oas.LibraryProcessingRule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	in, err := libraryProcessingInputFromOAS(req)
	if err != nil {
		return nil, err
	}
	created, err := svc.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	out := storedLibraryProcessingRuleToOAS(created)
	return &out, nil
}

// UpdateLibraryProcessingRule replaces an existing library processing rule (admin only).
func (h *Handlers) UpdateLibraryProcessingRule(
	ctx context.Context,
	req *oas.LibraryProcessingRuleInput,
	params oas.UpdateLibraryProcessingRuleParams,
) (*oas.LibraryProcessingRule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	in, err := libraryProcessingInputFromOAS(req)
	if err != nil {
		return nil, err
	}
	updated, err := svc.Update(ctx, params.RuleId, in)
	if err != nil {
		return nil, err
	}
	out := storedLibraryProcessingRuleToOAS(updated)
	return &out, nil
}

// DeleteLibraryProcessingRule deletes a stored library processing rule (admin only).
func (h *Handlers) DeleteLibraryProcessingRule(
	ctx context.Context,
	params oas.DeleteLibraryProcessingRuleParams,
) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return err
	}
	return svc.Delete(ctx, params.RuleId)
}

// ResolveLibraryProcessingRule selects the highest-precedence matching rule for the requested context.
func (h *Handlers) ResolveLibraryProcessingRule(
	ctx context.Context,
	params oas.ResolveLibraryProcessingRuleParams,
) (*oas.LibraryProcessingResolvedRule, error) {
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	res, err := svc.Resolve(ctx, libraryProcessingResolveQueryFromOAS(params))
	if err != nil {
		return nil, err
	}
	return &oas.LibraryProcessingResolvedRule{
		RuleID: res.RuleID,
		Scope:  oas.LibraryProcessingScope(res.Scope.String()),
		Rule:   libraryProcessingRuleBodyToOAS(res.Rule),
	}, nil
}

func libraryProcessingInputFromOAS(req *oas.LibraryProcessingRuleInput) (libraryprocessing.RuleInput, error) {
	scope, err := libraryprocessing.ParseScope(string(req.Scope))
	if err != nil {
		return libraryprocessing.RuleInput{}, err
	}
	in := libraryprocessing.RuleInput{
		Scope: scope,
		Rule: libraryprocessing.Rule{
			Track: libraryprocessing.TrackRule{
				Languages: slices.Clone(req.Rule.Track.Languages),
			},
		},
	}
	if instID, ok := req.InstanceID.Get(); ok {
		in.InstanceID = &instID
	}
	if itemID, ok := req.ItemID.Get(); ok {
		in.ItemID = &itemID
	}
	if userLang, ok := req.UserLanguage.Get(); ok {
		in.UserLanguage = &userLang
	}
	if rootPath, ok := req.RootPath.Get(); ok {
		in.RootPath = &rootPath
	}
	if mtVal, ok := req.MediaType.Get(); ok {
		mt := libraryprocessing.MediaType(mtVal)
		in.MediaType = &mt
	}
	return in, nil
}

func libraryProcessingResolveQueryFromOAS(params oas.ResolveLibraryProcessingRuleParams) libraryprocessing.ResolveQuery {
	var q libraryprocessing.ResolveQuery
	if instID, ok := params.InstanceID.Get(); ok {
		q.InstanceID = &instID
	}
	if itemID, ok := params.ItemID.Get(); ok {
		q.ItemID = &itemID
	}
	if reqLang, ok := params.RequesterLanguage.Get(); ok {
		q.RequesterLanguage = &reqLang
	}
	if rootPath, ok := params.RootPath.Get(); ok {
		q.RootPath = &rootPath
	}
	if mtVal, ok := params.MediaType.Get(); ok {
		mt := libraryprocessing.MediaType(mtVal)
		q.MediaType = &mt
	}
	return q
}

func libraryProcessingRuleBodyToOAS(r libraryprocessing.Rule) oas.Rule {
	langs := slices.Clone(r.Track.Languages)
	if langs == nil {
		langs = []string{}
	}
	return oas.Rule{
		Track: oas.TrackRule{
			Languages: langs,
		},
	}
}

func storedLibraryProcessingRuleToOAS(r libraryprocessing.StoredRule) oas.LibraryProcessingRule {
	out := oas.LibraryProcessingRule{
		ID:        r.ID,
		Scope:     oas.LibraryProcessingScope(r.Scope.String()),
		Rule:      libraryProcessingRuleBodyToOAS(r.Rule),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.InstanceID != nil {
		out.InstanceID = oas.NewOptUUID(*r.InstanceID)
	}
	if r.ItemID != nil {
		out.ItemID = oas.NewOptString(*r.ItemID)
	}
	if r.UserLanguage != nil {
		out.UserLanguage = oas.NewOptString(*r.UserLanguage)
	}
	if r.RootPath != nil {
		out.RootPath = oas.NewOptString(*r.RootPath)
	}
	if r.MediaType != nil {
		out.MediaType = oas.NewOptLibraryProcessingMediaType(oas.LibraryProcessingMediaType(*r.MediaType))
	}
	return out
}
