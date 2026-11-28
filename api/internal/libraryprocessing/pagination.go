// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// DefaultRulePageLimit is the default page size when limit is omitted (0).
	DefaultRulePageLimit = 50
	// MaxRulePageLimit is the maximum allowed page size for bounded rule pages.
	MaxRulePageLimit = 200
	// MaxRuleCursorLen bounds accepted opaque cursor string length in bytes.
	MaxRuleCursorLen = 512

	ruleCursorVersion        = 1
	defaultSnapshotBatchSize = MaxRulePageLimit
	maxSnapshotBatches       = 10_000
)

// PageQuery carries bounded keyset pagination parameters for listing stored rules.
type PageQuery struct {
	Limit  int
	Cursor *string
}

// RuleCursorKey holds the decoded keyset tuple (scope precedence rank, created_at, id).
type RuleCursorKey struct {
	Scope     Scope
	Rank      int32
	CreatedAt time.Time
	ID        uuid.UUID
}

// RulePage holds one bounded page of stored rules and final-page metadata.
type RulePage struct {
	Items      []StoredRule
	HasMore    bool
	NextCursor string
}

type wireRuleCursor struct {
	Version   int    `json:"v"`
	Scope     string `json:"s"`
	CreatedAt string `json:"c"`
	ID        string `json:"i"`
}

// ScopeRank maps a validated Scope to its SQL precedence rank (1 = highest priority).
func ScopeRank(s Scope) (int32, error) {
	switch s {
	case ScopeItemOverride:
		return 1, nil
	case ScopeUserLanguage:
		return 2, nil
	case ScopeLibrary:
		return 3, nil
	case ScopeInstance:
		return 4, nil
	case ScopeMediaType:
		return 5, nil
	case ScopeGlobalDefault:
		return 6, nil
	default:
		return 0, fmt.Errorf("%w: invalid scope %q", domain.ErrInvalid, s.String())
	}
}

// ResolveRulePageLimit applies DefaultRulePageLimit when limit is 0 and enforces 1..MaxRulePageLimit.
func ResolveRulePageLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultRulePageLimit, nil
	}
	if limit < 1 || limit > MaxRulePageLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalid, MaxRulePageLimit)
	}
	return limit, nil
}

// EncodeRuleCursor builds an opaque URL-safe keyset cursor from a stored rule's sort key.
func EncodeRuleCursor(r StoredRule) (string, error) {
	if _, err := ScopeRank(r.Scope); err != nil {
		return "", err
	}
	if r.CreatedAt.IsZero() {
		return "", fmt.Errorf("%w: cursor created_at is required", domain.ErrInvalid)
	}
	if r.ID == uuid.Nil {
		return "", fmt.Errorf("%w: cursor id is required", domain.ErrInvalid)
	}
	createdAt := time.UnixMicro(r.CreatedAt.UnixMicro()).UTC()
	payload, err := json.Marshal(wireRuleCursor{
		Version:   ruleCursorVersion,
		Scope:     r.Scope.String(),
		CreatedAt: createdAt.Format(time.RFC3339Nano),
		ID:        r.ID.String(),
	})
	if err != nil {
		return "", fmt.Errorf("libraryprocessing: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeRuleCursor parses and validates an opaque rule pagination cursor.
func DecodeRuleCursor(raw string) (RuleCursorKey, error) {
	w, err := decodeWireRuleCursor(raw)
	if err != nil {
		return RuleCursorKey{}, err
	}
	return parseRuleCursorFields(w)
}

func decodeCursorBase64(raw string) ([]byte, error) {
	if raw == "" || len(raw) > MaxRuleCursorLen || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("%w: invalid cursor encoding", domain.ErrInvalid)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor encoding", domain.ErrInvalid)
	}
	return decoded, nil
}

func decodeWireRuleCursor(raw string) (wireRuleCursor, error) {
	decoded, err := decodeCursorBase64(raw)
	if err != nil {
		return wireRuleCursor{}, err
	}
	trimmed := bytes.Trim(decoded, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return wireRuleCursor{}, fmt.Errorf("%w: invalid cursor payload", domain.ErrInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var w wireRuleCursor
	if err := dec.Decode(&w); err != nil {
		return wireRuleCursor{}, fmt.Errorf("%w: invalid cursor payload", domain.ErrInvalid)
	}
	if len(bytes.Trim(trimmed[dec.InputOffset():], " \t\r\n")) != 0 {
		return wireRuleCursor{}, fmt.Errorf("%w: trailing data in cursor payload", domain.ErrInvalid)
	}
	return w, nil
}

func parseRuleCursorFields(w wireRuleCursor) (RuleCursorKey, error) {
	if w.Version != ruleCursorVersion {
		return RuleCursorKey{}, fmt.Errorf("%w: unsupported cursor version", domain.ErrInvalid)
	}
	scope, err := ParseScope(w.Scope)
	if err != nil {
		return RuleCursorKey{}, fmt.Errorf("cursor scope: %w", err)
	}
	rank, err := ScopeRank(scope)
	if err != nil {
		return RuleCursorKey{}, err
	}
	createdAt, err := parseCursorTimestamp(w.CreatedAt)
	if err != nil {
		return RuleCursorKey{}, err
	}
	id, err := parseCursorUUID(w.ID)
	if err != nil {
		return RuleCursorKey{}, err
	}
	return RuleCursorKey{
		Scope:     scope,
		Rank:      rank,
		CreatedAt: createdAt,
		ID:        id,
	}, nil
}

func parseCursorTimestamp(raw string) (time.Time, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return time.Time{}, fmt.Errorf("%w: invalid cursor timestamp", domain.ErrInvalid)
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || parsed.IsZero() || parsed.Nanosecond()%1000 != 0 {
		return time.Time{}, fmt.Errorf("%w: invalid cursor timestamp", domain.ErrInvalid)
	}
	return parsed.UTC(), nil
}

func parseCursorUUID(raw string) (uuid.UUID, error) {
	if len(raw) != 36 || strings.ToLower(raw) != raw {
		return uuid.Nil, fmt.Errorf("%w: invalid cursor id", domain.ErrInvalid)
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invalid cursor id", domain.ErrInvalid)
	}
	return id, nil
}

func parsePageQuery(q PageQuery) (int, *RuleCursorKey, error) {
	limit, err := ResolveRulePageLimit(q.Limit)
	if err != nil {
		return 0, nil, err
	}
	if q.Cursor == nil {
		return limit, nil, nil
	}
	key, err := DecodeRuleCursor(*q.Cursor)
	if err != nil {
		return 0, nil, err
	}
	return limit, &key, nil
}

func storedRulesFromRows(rows []sqlcgen.LibraryProcessingRule) ([]StoredRule, error) {
	out := make([]StoredRule, 0, len(rows))
	for _, row := range rows {
		sr, err := storedRuleFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, nil
}

func queryRuleBatch(
	ctx context.Context,
	q *sqlcgen.Queries,
	limit int,
	key *RuleCursorKey,
) ([]StoredRule, bool, error) {
	params := sqlcgen.ListLibraryProcessingRulesKeysetParams{
		AfterCreatedAt: time.Unix(0, 0).UTC(),
		PageLimit:      int32(limit) + 1, // #nosec G115 -- limit is bounded 1..MaxRulePageLimit (200)
	}
	if key != nil {
		params.UseCursor = true
		params.AfterRank = key.Rank
		params.AfterCreatedAt = key.CreatedAt
		params.AfterID = key.ID
	}
	rows, err := q.ListLibraryProcessingRulesKeyset(ctx, params)
	if err != nil {
		return nil, false, fmt.Errorf("libraryprocessing: list rules keyset: %w", store.MapError(err))
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items, err := storedRulesFromRows(rows)
	if err != nil {
		return nil, false, err
	}
	return items, hasMore, nil
}

func cursorKeyFromRule(r StoredRule) (RuleCursorKey, error) {
	rank, err := ScopeRank(r.Scope)
	if err != nil {
		return RuleCursorKey{}, err
	}
	return RuleCursorKey{
		Scope:     r.Scope,
		Rank:      rank,
		CreatedAt: time.UnixMicro(r.CreatedAt.UnixMicro()).UTC(),
		ID:        r.ID,
	}, nil
}

// ListPage returns one bounded keyset page of stored rules in precedence order.
func (s *Service) ListPage(ctx context.Context, q PageQuery) (RulePage, error) {
	opCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(opCtx); err != nil {
		return RulePage{}, err
	}
	limit, key, err := parsePageQuery(q)
	if err != nil {
		return RulePage{}, err
	}
	items, hasMore, err := queryRuleBatch(opCtx, s.st.Q(), limit, key)
	if err != nil {
		return RulePage{}, err
	}
	var nextCursor string
	if hasMore && len(items) > 0 {
		nextCursor, err = EncodeRuleCursor(items[len(items)-1])
		if err != nil {
			return RulePage{}, err
		}
	}
	return RulePage{
		Items:      items,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func resolveSnapshotBatchSize(batchSize int) int {
	if batchSize <= 0 || batchSize > MaxRulePageLimit {
		return defaultSnapshotBatchSize
	}
	return batchSize
}

func streamSnapshotBatches(
	ctx context.Context,
	q *sqlcgen.Queries,
	limit int,
	onBatch func([]StoredRule) error,
) error {
	var cursor *RuleCursorKey
	for range maxSnapshotBatches {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, hasMore, err := queryRuleBatch(ctx, q, limit, cursor)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			if batchErr := onBatch(items); batchErr != nil {
				return batchErr
			}
		}
		if !hasMore || len(items) == 0 {
			return nil
		}
		nextKey, err := cursorKeyFromRule(items[len(items)-1])
		if err != nil {
			return err
		}
		cursor = &nextKey
	}
	return fmt.Errorf("libraryprocessing: rule count exceeds snapshot batch bound (%d batches)", maxSnapshotBatches)
}

// StreamAll reads all stored rules in precedence order across bounded keyset batches
// within a single repeatable-read snapshot transaction, invoking onBatch per non-empty batch.
// The module check and all batches share one operationTimeout deadline.
func (s *Service) StreamAll(ctx context.Context, batchSize int, onBatch func([]StoredRule) error) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	limit := resolveSnapshotBatchSize(batchSize)
	return s.st.SnapshotReadTx(ctx, func(q *sqlcgen.Queries) error {
		return streamSnapshotBatches(ctx, q, limit, onBatch)
	})
}
