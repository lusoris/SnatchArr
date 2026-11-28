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

const legacyRuleStreamBatchSize = libraryprocessing.MaxRulePageLimit

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
	if err = validateRawQuery(ctx); err != nil {
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
	w                http.ResponseWriter
	rc               *http.ResponseController
	responseDeadline time.Time
	buf              bytes.Buffer
	pending          []libraryprocessing.StoredRule
	committed        bool
	wroteItem        bool
}

func (sw *ruleArrayStreamWriter) controller() *http.ResponseController {
	if sw.rc == nil {
		sw.rc = http.NewResponseController(sw.w)
	}
	return sw.rc
}

func (sw *ruleArrayStreamWriter) flush() error {
	// Plain JSON also works through writers without flush support.
	if err := sw.controller().Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("httpapi: flush library processing rules: %w", err)
	}
	return nil
}

func (sw *ruleArrayStreamWriter) onBatch(ctx context.Context, batch []libraryprocessing.StoredRule) error {
	if len(sw.pending) > 0 {
		if err := sw.flushPending(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
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
}

func (sw *ruleArrayStreamWriter) encodeChunk(closeArray bool) error {
	sw.buf.Reset()
	if !sw.committed {
		sw.buf.WriteByte('[')
	}
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
	if closeArray {
		sw.buf.WriteByte(']')
	}
	return nil
}

func (sw *ruleArrayStreamWriter) writeChunk(ctx context.Context, closeArray bool) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	// Apply the chunk deadline only for this write and flush, then restore the response limit.
	restoreDeadline, err := applyStreamWriteDeadline(sw.controller(), ctx, sw.responseDeadline)
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := restoreDeadline(); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()
	if err = sw.encodeChunk(closeArray); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	sw.commitHeaders()
	if _, writeErr := sw.w.Write(sw.buf.Bytes()); writeErr != nil {
		return fmt.Errorf("httpapi: write library processing rules: %w", writeErr)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return sw.flush()
}

func (sw *ruleArrayStreamWriter) flushPending(ctx context.Context) error {
	return sw.writeChunk(ctx, false)
}

func (sw *ruleArrayStreamWriter) finish(ctx context.Context) error {
	return sw.writeChunk(ctx, true)
}

// ServeLibraryProcessingRules streams the legacy full JSON array in bounded batches over
// a repeatable-read snapshot, preserving RFC 9457 errors before headers commit and aborting
// mid-stream failures without emitting a valid closing bracket.
//
// Transaction and deadline boundary:
//   - Response bound: SnatchArr's default native HTTP server (golusoris httpx/server) sets
//     WriteTimeout to 60s and exposes *http.Server via http.ServerContextKey. From handler
//     entry, ruleStreamResponseContext bounds total response streaming by the server's positive
//     WriteTimeout or any earlier caller deadline on r.Context(), while preserving parent
//     request cancellation. When no positive server WriteTimeout is present, the response bound
//     is the caller deadline alone, or unbounded (zero write deadline) if none was set.
//   - Snapshot and intermediate writes: module gate checks, SnapshotReadTx queries and commit,
//     and intermediate batch flushes (flushPending) run under the 5s service operation budget
//     capped by the response deadline. Each intermediate flush arms that snapshot deadline and
//     restores the response deadline before returning (finite server/caller deadline when
//     bounded, or zero when unbounded), so the 5s snapshot deadline never stays armed on the
//     connection during SnapshotReadTx.Commit.
//   - Post-commit final write: once SnapshotReadTx commits, the final buffered batch and closing
//     ']' (finish) run under the response context independently of the 5s snapshot budget,
//     capped by the server WriteTimeout or earlier caller deadline, and keep any finite response
//     deadline armed even if the final write or flush fails.
//
// Middleware and writer support:
//   - SnatchArr's production stack (Recover, Logger via httpsnoop.Wrap) preserves Unwrap,
//     Flush/FlushError, and SetWriteDeadline for net/http HTTP/1.1 and HTTP/2 writers, and
//     Recover re-panics http.ErrAbortHandler without writing a recovery body.
//   - Limitations: writers without SetWriteDeadline or Flush/FlushError (such as custom
//     wrappers lacking Unwrap or httptest.ResponseRecorder) return http.ErrNotSupported and
//     degrade to context-only checks without socket deadlines or incremental flushing.
//   - net/http exposes no getter for the pre-existing connection write deadline, which starts
//     when request headers finish reading before middleware runs. Bounding by handler entry plus
//     srv.WriteTimeout therefore adds preceding middleware time rather than restoring the exact
//     initial connection deadline. Only an unbounded response restores a zero deadline.
func (h *Handlers) ServeLibraryProcessingRules() http.HandlerFunc {
	return h.serveLibraryProcessingRulesBatch(legacyRuleStreamBatchSize)
}

type ruleStreamFunc func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error

type snapshotContextFunc func(ctx context.Context) (context.Context, context.CancelFunc)

func (h *Handlers) serveLibraryProcessingRulesBatch(batchSize int) http.HandlerFunc {
	return h.serveLibraryProcessingRulesWith(batchSize, nil)
}

func noopCancel() {}

func ruleStreamResponseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if srv, ok := ctx.Value(http.ServerContextKey).(*http.Server); ok && srv != nil && srv.WriteTimeout > 0 {
		// WithTimeout retains any earlier parent deadline and propagates parent cancellation.
		return context.WithTimeout(ctx, srv.WriteTimeout)
	}
	return ctx, noopCancel
}

func noopRestoreDeadline() error { return nil }

func applyStreamWriteDeadline(rc *http.ResponseController, ctx context.Context, responseDeadline time.Time) (func() error, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return noopRestoreDeadline, nil
	}
	if err := rc.SetWriteDeadline(deadline); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			return noopRestoreDeadline, nil
		}
		return nil, fmt.Errorf("httpapi: set library processing rules write deadline: %w", err)
	}
	if deadline.Equal(responseDeadline) {
		return noopRestoreDeadline, nil
	}
	return func() error {
		if err := rc.SetWriteDeadline(responseDeadline); err != nil {
			if responseDeadline.IsZero() {
				return fmt.Errorf("httpapi: clear library processing rules write deadline: %w", err)
			}
			return fmt.Errorf("httpapi: restore library processing rules write deadline: %w", err)
		}
		return nil
	}, nil
}

func streamRuleArray(
	responseCtx, snapshotCtx context.Context,
	w http.ResponseWriter,
	batchSize int,
	streamFn ruleStreamFunc,
) (bool, error) {
	if err := snapshotCtx.Err(); err != nil {
		return false, err
	}
	responseDeadline, _ := responseCtx.Deadline()
	sw := &ruleArrayStreamWriter{
		w:                w,
		rc:               http.NewResponseController(w),
		responseDeadline: responseDeadline,
	}
	if err := streamFn(snapshotCtx, batchSize, func(batch []libraryprocessing.StoredRule) error {
		return sw.onBatch(snapshotCtx, batch)
	}); err != nil {
		return sw.committed, err
	}
	// A committed snapshot no longer governs output; the response context still does.
	err := sw.finish(responseCtx)
	return sw.committed, err
}

func (h *Handlers) serveLibraryProcessingRulesWith(batchSize int, override ruleStreamFunc) http.HandlerFunc {
	return h.serveLibraryProcessingRulesAt(batchSize, nil, override)
}

func (h *Handlers) serveLibraryProcessingRulesAt(
	batchSize int,
	withSnapshot snapshotContextFunc,
	override ruleStreamFunc,
) http.HandlerFunc {
	if withSnapshot == nil {
		withSnapshot = libraryprocessing.WithOperationDeadline
	}
	logger := h.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	onErr := ErrorHandler(logger)
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx := r.Context()
		responseCtx, cancelResponse := ruleStreamResponseContext(reqCtx)
		defer cancelResponse()
		streamCtx, cancel := withSnapshot(responseCtx)
		defer cancel()
		svc, err := h.checkLibraryProcessingEnabled(streamCtx)
		if err != nil {
			onErr(reqCtx, w, r, err)
			return
		}
		streamFn := svc.StreamAll
		if override != nil {
			streamFn = override
		}
		committed, err := streamRuleArray(responseCtx, streamCtx, w, batchSize, streamFn)
		if err != nil {
			h.failRuleArrayStream(reqCtx, w, r, onErr, committed, err)
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
	enabled, err := h.modules.IsEnabled(ctx, domain.ModuleLibraryProcessing)
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

func validateRawQuery(ctx context.Context) error {
	// Generated typed params drop url.ParseQuery errors from URL.Query(), so
	// reject malformed query strings here. Direct callers without an HTTP
	// request in the context are unaffected.
	if r, ok := requestFrom(ctx); ok && r != nil && r.URL != nil {
		if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
			return fmt.Errorf("%w: malformed query string", domain.ErrBadRequest)
		}
	}
	return nil
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
	if err = validateRawQuery(ctx); err != nil {
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
