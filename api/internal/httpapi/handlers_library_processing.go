// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

const (
	legacyRuleStreamBatchSize = libraryprocessing.MaxRulePageLimit
	moduleGateCheckTimeout    = 10 * time.Second
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

// ListLibraryProcessingRulesPage returns one bounded keyset page of stored rules in precedence order.
func (h *Handlers) ListLibraryProcessingRulesPage(
	ctx context.Context,
	params oas.ListLibraryProcessingRulesPageParams,
) (*oas.LibraryProcessingRulePage, error) {
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	var q libraryprocessing.PageQuery
	if limit, ok := params.Limit.Get(); ok {
		q.Limit = limit
	}
	if cursor, ok := params.Cursor.Get(); ok {
		q.Cursor = &cursor
	}
	page, err := svc.ListPage(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]oas.LibraryProcessingRule, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, storedLibraryProcessingRuleToOAS(r))
	}
	out := &oas.LibraryProcessingRulePage{
		Items:   items,
		HasMore: page.HasMore,
	}
	if page.HasMore && page.NextCursor != "" {
		out.NextCursor = oas.NewOptString(page.NextCursor)
	}
	return out, nil
}

type ruleArrayStreamWriter struct {
	w         http.ResponseWriter
	buf       bytes.Buffer
	pending   []libraryprocessing.StoredRule
	committed bool
	wroteItem bool
}

func (sw *ruleArrayStreamWriter) onBatch(batch []libraryprocessing.StoredRule) error {
	if len(sw.pending) > 0 {
		if err := sw.flushPending(); err != nil {
			return err
		}
	}
	sw.pending = batch
	return nil
}

func (sw *ruleArrayStreamWriter) commitHeaders() {
	if sw.committed {
		return
	}
	sw.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sw.w.WriteHeader(http.StatusOK)
	sw.committed = true
	sw.buf.WriteByte('[')
}

func (sw *ruleArrayStreamWriter) encodePending() error {
	for _, item := range sw.pending {
		oasItem := storedLibraryProcessingRuleToOAS(item)
		raw, err := oasItem.MarshalJSON()
		if err != nil {
			return fmt.Errorf("httpapi: encode library processing rule: %w", err)
		}
		if sw.wroteItem {
			sw.buf.WriteByte(',')
		}
		sw.buf.Write(raw)
		sw.wroteItem = true
	}
	sw.pending = nil
	return nil
}

func (sw *ruleArrayStreamWriter) flushPending() error {
	sw.buf.Reset()
	sw.commitHeaders()
	if err := sw.encodePending(); err != nil {
		return err
	}
	if _, err := sw.w.Write(sw.buf.Bytes()); err != nil {
		return fmt.Errorf("httpapi: write library processing rules: %w", err)
	}
	// Plain JSON also works through writers without flush support.
	if err := http.NewResponseController(sw.w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("httpapi: flush library processing rules: %w", err)
	}
	return nil
}

func (sw *ruleArrayStreamWriter) finish() error {
	sw.buf.Reset()
	sw.commitHeaders()
	if err := sw.encodePending(); err != nil {
		return err
	}
	sw.buf.WriteByte(']')
	if _, err := sw.w.Write(sw.buf.Bytes()); err != nil {
		return fmt.Errorf("httpapi: finish library processing rules: %w", err)
	}
	return nil
}

// ServeLibraryProcessingRules streams the legacy full JSON array in bounded batches over
// a repeatable-read snapshot, preserving RFC 9457 errors before headers commit and aborting
// mid-stream failures without emitting a valid closing bracket.
func (h *Handlers) ServeLibraryProcessingRules() http.HandlerFunc {
	return h.serveLibraryProcessingRulesBatch(legacyRuleStreamBatchSize)
}

func (h *Handlers) serveLibraryProcessingRulesBatch(batchSize int) http.HandlerFunc {
	onErr := ErrorHandler(h.logger)
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		svc, err := h.checkLibraryProcessingEnabled(ctx)
		if err != nil {
			onErr(ctx, w, r, err)
			return
		}
		sw := &ruleArrayStreamWriter{w: w}
		err = svc.StreamAll(ctx, batchSize, sw.onBatch)
		if err == nil {
			err = sw.finish()
		}
		if err != nil {
			h.failRuleArrayStream(ctx, w, r, onErr, sw.committed, err)
		}
	}
}

func (h *Handlers) checkLibraryProcessingEnabled(ctx context.Context) (*libraryprocessing.Service, error) {
	svc, err := h.requireLibraryProcessing()
	if err != nil {
		return nil, err
	}
	if h.modules == nil {
		return svc, nil
	}
	gateCtx, cancel := context.WithTimeout(ctx, moduleGateCheckTimeout)
	defer cancel()
	enabled, err := h.modules.IsEnabled(gateCtx, domain.ModuleLibraryProcessing)
	if err != nil {
		return nil, fmt.Errorf("module gate %s: %w", domain.ModuleLibraryProcessing, err)
	}
	if !enabled {
		return nil, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleLibraryProcessing)
	}
	return svc, nil
}

func (h *Handlers) failRuleArrayStream(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	onErr func(context.Context, http.ResponseWriter, *http.Request, error),
	committed bool,
	err error,
) {
	if !committed {
		onErr(ctx, w, r, err)
		return
	}
	if h.logger != nil {
		h.logger.ErrorContext(ctx, "httpapi: stream library processing rules aborted",
			slog.String("error", err.Error()),
		)
	}
	panic(http.ErrAbortHandler)
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
	// Generated typed params drop url.ParseQuery errors from URL.Query(), so
	// reject malformed query strings here. Direct callers without an HTTP
	// request in the context are unaffected.
	if r, ok := requestFrom(ctx); ok && r != nil && r.URL != nil {
		if _, perr := url.ParseQuery(r.URL.RawQuery); perr != nil {
			return nil, fmt.Errorf("%w: malformed query string", domain.ErrBadRequest)
		}
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
