// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package prowlarr monitors Prowlarr indexer stamina and limits (#38).
// It never writes to Prowlarr.
package prowlarr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/keyed"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Bounds (HISS-02/06).
const (
	StatusTTL              = 30 * time.Second
	MaxLinks               = 50
	MaxIndexersPerInstance = 500
	unchangedKey           = "<unchanged>"
)

// Input is the user-supplied part of a link.
type Input struct {
	Name      string
	BaseURL   string
	APIKey    string
	HourlyCap int
	Enabled   bool
}

// IndexerStatus is the live status of an indexer from Prowlarr.
type IndexerStatus struct {
	ID           int
	Name         string
	Enabled      bool
	QueryLimit   *int
	GrabLimit    *int
	LimitsUnit   int
	UsedQueries  int
	UsedGrabs    int
	DisabledTill *time.Time
	Exhausted    bool
	ExhaustedBy  string
}

// LinkStatus is one link's live view.
type LinkStatus struct {
	Link          domain.ProwlarrLink
	Reachable     bool
	Error         string
	Status        prowlarrclient.Status
	Indexers      []IndexerStatus
	MatchPrefixes []string
	CheckedAt     time.Time
}

type cachedArrIndexers struct {
	indexers  []arr.ProviderResource
	fetchedAt time.Time
}

// Service is the Prowlarr use-case layer and planner gate.
type Service struct {
	st        *store.Store
	enc       *crypto.Encryptor
	clk       clock.Clock
	ids       id.Generator
	client    prowlarrclient.Client
	discover  arrclient.IndexerDiscoverer
	instances *instances.Service
	logger    *slog.Logger
	cfg       config.ProwlarrOptions

	cache        keyed.Map[uuid.UUID, LinkStatus]
	indexerCache keyed.Map[uuid.UUID, cachedArrIndexers]
}

// New wires the service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator,
	client prowlarrclient.Client, discover arrclient.IndexerDiscoverer, inst *instances.Service,
	logger *slog.Logger, cfg config.Options,
) *Service {
	return &Service{
		st: st, enc: enc, clk: clk, ids: ids, client: client, discover: discover,
		instances: inst, logger: logger, cfg: cfg.Prowlarr,
	}
}

// List returns every link without secrets.
func (s *Service) List(ctx context.Context) ([]domain.ProwlarrLink, error) {
	rows, err := s.st.Q().ListProwlarrLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: list: %w", store.MapError(err))
	}
	out := make([]domain.ProwlarrLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.ProwlarrLinkFromRow(r))
	}
	return out, nil
}

// Get returns one link without its secret.
func (s *Service) Get(ctx context.Context, linkID uuid.UUID) (domain.ProwlarrLink, error) {
	row, err := s.st.Q().GetProwlarrLink(ctx, linkID)
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: get: %w", store.MapError(err))
	}
	return store.ProwlarrLinkFromRow(row), nil
}

// Create validates, encrypts and stores a link.
func (s *Service) Create(ctx context.Context, in Input) (domain.ProwlarrLink, error) {
	in, err := normalize(in)
	if err != nil {
		return domain.ProwlarrLink{}, err
	}
	linkID, err := s.ids.NewUUID()
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.APIKey))
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: seal api key: %w", err)
	}
	hourlyCap := int32(in.HourlyCap) // #nosec G115 -- validated 1..500
	row, err := s.st.Q().CreateProwlarrLink(ctx, sqlcgen.CreateProwlarrLinkParams{
		ID:        linkID,
		Name:      in.Name,
		BaseUrl:   in.BaseURL,
		ApiKeyEnc: sealed,
		HourlyCap: hourlyCap,
		Enabled:   in.Enabled,
		CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: create: %w", store.MapError(err))
	}
	return store.ProwlarrLinkFromRow(row), nil
}

// Update replaces the editable fields; an empty APIKey keeps the stored one.
func (s *Service) Update(ctx context.Context, linkID uuid.UUID, in Input) (domain.ProwlarrLink, error) {
	current, err := s.st.Q().GetProwlarrLink(ctx, linkID)
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: get: %w", store.MapError(err))
	}
	if in.APIKey == "" {
		in.APIKey = unchangedKey
	}
	if in.HourlyCap == 0 {
		in.HourlyCap = int(current.HourlyCap)
	}
	in, err = normalize(in)
	if err != nil {
		return domain.ProwlarrLink{}, err
	}
	sealed := current.ApiKeyEnc
	if in.APIKey != unchangedKey {
		if sealed, err = s.enc.Seal([]byte(in.APIKey)); err != nil {
			return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: seal api key: %w", err)
		}
	}
	hourlyCap := int32(in.HourlyCap) // #nosec G115 -- validated 1..500
	row, err := s.st.Q().UpdateProwlarrLink(ctx, sqlcgen.UpdateProwlarrLinkParams{
		ID:        linkID,
		Name:      in.Name,
		BaseUrl:   in.BaseURL,
		ApiKeyEnc: sealed,
		HourlyCap: hourlyCap,
		Enabled:   in.Enabled,
		UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.ProwlarrLink{}, fmt.Errorf("prowlarr: update: %w", store.MapError(err))
	}
	s.cache.Forget(linkID)
	return store.ProwlarrLinkFromRow(row), nil
}

// Delete removes a link.
func (s *Service) Delete(ctx context.Context, linkID uuid.UUID) error {
	n, err := s.st.Q().DeleteProwlarrLink(ctx, linkID)
	if err != nil {
		return fmt.Errorf("prowlarr: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("prowlarr: delete: %w", domain.ErrNotFound)
	}
	s.cache.Forget(linkID)
	return nil
}

// Test probes a stored link now and records the outcome.
func (s *Service) Test(ctx context.Context, linkID uuid.UUID) (prowlarrclient.Status, error) {
	row, err := s.st.Q().GetProwlarrLink(ctx, linkID)
	if err != nil {
		return prowlarrclient.Status{}, fmt.Errorf("prowlarr: get: %w", store.MapError(err))
	}
	view := s.observe(ctx, row)
	if !view.Reachable {
		return prowlarrclient.Status{}, fmt.Errorf("prowlarr: test: %w: %s", domain.ErrUnreachable, view.Error)
	}
	return view.Status, nil
}

// TestInput probes credentials that are not stored yet.
func (s *Service) TestInput(ctx context.Context, in Input) (prowlarrclient.Status, error) {
	in, err := normalize(in)
	if err != nil {
		return prowlarrclient.Status{}, err
	}
	st, err := s.client.Status(ctx, in.BaseURL, in.APIKey)
	if err != nil {
		return st, fmt.Errorf("prowlarr: test input: %w", err)
	}
	return st, nil
}

// Status returns the (cached) live view of every enabled link.
func (s *Service) Status(ctx context.Context) ([]LinkStatus, error) {
	rows, err := s.st.Q().ListEnabledProwlarrLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: status: %w", store.MapError(err))
	}
	out := make([]LinkStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.cachedObserve(ctx, row))
	}
	return out, nil
}

func (s *Service) cachedObserve(ctx context.Context, row sqlcgen.ProwlarrLink) LinkStatus {
	view, ok := s.cache.Get(row.ID)
	if ok && s.clk.Now().Before(view.CheckedAt.Add(StatusTTL)) {
		return view
	}
	return s.observe(ctx, row)
}

func (s *Service) observe(ctx context.Context, row sqlcgen.ProwlarrLink) LinkStatus {
	view := LinkStatus{
		Link:          store.ProwlarrLinkFromRow(row),
		MatchPrefixes: []string{row.BaseUrl},
		CheckedAt:     s.clk.Now(),
	}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		view.Error = "stored api key cannot be decrypted"
		return s.remember(ctx, view)
	}
	apiKey := string(key)
	if view.Status, err = s.client.Status(ctx, row.BaseUrl, apiKey); err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	indexers, err := s.client.Indexers(ctx, row.BaseUrl, apiKey)
	if err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	statuses, err := s.client.Statuses(ctx, row.BaseUrl, apiKey)
	if err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	appURLs, err := s.client.ProwlarrURLs(ctx, row.BaseUrl, apiKey)
	if err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	view.MatchPrefixes = buildMatchPrefixes(row.BaseUrl, appURLs)
	view.Indexers, err = s.buildIndexerStatuses(ctx, row.BaseUrl, apiKey, indexers, statuses)
	if err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	view.Reachable = true
	return s.remember(ctx, view)
}

func buildMatchPrefixes(baseURL string, appURLs []string) []string {
	prefixes := make([]string, 0, 1+len(appURLs))
	seen := make(map[string]struct{}, 1+len(appURLs))
	if b := strings.TrimSpace(baseURL); b != "" {
		seen[b] = struct{}{}
		prefixes = append(prefixes, b)
	}
	for _, u := range appURLs {
		t := strings.TrimSpace(u)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			prefixes = append(prefixes, t)
		}
	}
	return prefixes
}

func (s *Service) buildIndexerStatuses(ctx context.Context, baseURL, apiKey string,
	indexers []prowlarrclient.Indexer, statuses map[int]time.Time,
) ([]IndexerStatus, error) {
	now := s.clk.Now()
	hasHour := false
	for _, idx := range indexers {
		if idx.LimitsUnit == 1 {
			hasHour = true
			break
		}
	}
	usageDay, err := s.client.Usage(ctx, baseURL, apiKey, now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	usageHour := usageDay
	if hasHour {
		if usageHour, err = s.client.Usage(ctx, baseURL, apiKey, now.Add(-time.Hour), now); err != nil {
			return nil, err
		}
	}
	threshold := s.cfg.LimitThreshold
	out := make([]IndexerStatus, 0, len(indexers))
	for _, idx := range indexers {
		out = append(out, s.evalIndexerStatus(idx, statuses, usageDay, usageHour, now, threshold))
	}
	return out, nil
}

func isLimitExceeded(limit *int, used int, threshold float64) bool {
	return limit != nil && *limit > 0 && float64(used) >= threshold*float64(*limit)
}

func determineExhaustion(idx prowlarrclient.Indexer, u prowlarrclient.Usage, disabledTill *time.Time, now time.Time, threshold float64) string {
	switch {
	case isLimitExceeded(idx.QueryLimit, u.Queries, threshold):
		return "query_limit"
	case isLimitExceeded(idx.GrabLimit, u.Grabs, threshold):
		return "grab_limit"
	case !idx.Enabled || (disabledTill != nil && disabledTill.After(now)):
		return "disabled"
	default:
		return ""
	}
}

func (s *Service) evalIndexerStatus(idx prowlarrclient.Indexer, statuses map[int]time.Time,
	usageDay, usageHour map[int]prowlarrclient.Usage, now time.Time, threshold float64,
) IndexerStatus {
	u := usageDay[idx.ID]
	if idx.LimitsUnit == 1 {
		u = usageHour[idx.ID]
	}
	var disabledTill *time.Time
	if dt, ok := statuses[idx.ID]; ok && !dt.IsZero() {
		disabledTill = &dt
	}
	exhaustedBy := determineExhaustion(idx, u, disabledTill, now, threshold)
	return IndexerStatus{
		ID:           idx.ID,
		Name:         idx.Name,
		Enabled:      idx.Enabled,
		QueryLimit:   idx.QueryLimit,
		GrabLimit:    idx.GrabLimit,
		LimitsUnit:   idx.LimitsUnit,
		UsedQueries:  u.Queries,
		UsedGrabs:    u.Grabs,
		DisabledTill: disabledTill,
		Exhausted:    exhaustedBy != "",
		ExhaustedBy:  exhaustedBy,
	}
}

func (s *Service) remember(ctx context.Context, view LinkStatus) LinkStatus {
	s.cache.Put(view.Link.ID, view)
	var lastErr, version *string
	if view.Reachable {
		version = &view.Status.Version
	} else {
		msg := view.Error
		lastErr = &msg
	}
	err := s.st.Q().RecordProwlarrCheck(ctx, sqlcgen.RecordProwlarrCheckParams{
		ID: view.Link.ID, LastSeenVersion: version, LastCheckAt: &view.CheckedAt, LastError: lastErr,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "prowlarr: record check", slog.String("error", err.Error()))
	}
	return view
}

func (s *Service) getArrIndexers(ctx context.Context, inst domain.Instance) ([]arr.ProviderResource, error) {
	if cached, ok := s.indexerCache.Get(inst.ID); ok {
		if s.clk.Now().Before(cached.fetchedAt.Add(StatusTTL)) {
			return cached.indexers, nil
		}
	}
	creds, err := s.instances.Credentials(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	arrIndexers, err := s.discover.Indexers(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	if err != nil {
		return nil, err
	}
	if len(arrIndexers) > MaxIndexersPerInstance {
		arrIndexers = arrIndexers[:MaxIndexersPerInstance]
	}
	s.indexerCache.Put(inst.ID, cachedArrIndexers{
		indexers:  arrIndexers,
		fetchedAt: s.clk.Now(),
	})
	return arrIndexers, nil
}

// Allow implements snatch.Gate.
func (s *Service) Allow(ctx context.Context, inst domain.Instance, _ domain.SnatchKind) (bool, snatch.GateRefusal, error) {
	rows, err := s.st.Q().ListEnabledProwlarrLinks(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "prowlarr: list links failed, failing open", slog.String("error", err.Error()))
		return true, snatch.GateRefusal{}, nil
	}
	if len(rows) == 0 {
		return true, snatch.GateRefusal{}, nil
	}
	if len(rows) > MaxLinks {
		rows = rows[:MaxLinks]
	}
	arrIndexers, err := s.getArrIndexers(ctx, inst)
	if err != nil {
		s.logger.WarnContext(ctx, "prowlarr: *arr indexers failed, failing open", slog.String("instance", inst.Name), slog.String("error", err.Error()))
		return true, snatch.GateRefusal{}, nil
	}
	return s.evaluateGate(ctx, inst, rows, arrIndexers)
}

type matchedIndexer struct {
	linkID   uuid.UUID
	linkName string
	id       int
}

func filterAutoSearchIndexers(arrIndexers []arr.ProviderResource) []arr.ProviderResource {
	out := make([]arr.ProviderResource, 0, len(arrIndexers))
	for _, p := range arrIndexers {
		if p.EnableAutomaticSearch == nil || *p.EnableAutomaticSearch {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) evaluateGate(ctx context.Context, inst domain.Instance, links []sqlcgen.ProwlarrLink, arrIndexers []arr.ProviderResource) (bool, snatch.GateRefusal, error) {
	autoSearch := filterAutoSearchIndexers(arrIndexers)
	if len(autoSearch) == 0 {
		return true, snatch.GateRefusal{}, nil
	}
	linkViews := make(map[uuid.UUID]LinkStatus, len(links))
	for _, link := range links {
		linkViews[link.ID] = s.cachedObserve(ctx, link)
	}
	matched, hasUnmatched := matchIndexers(autoSearch, links, linkViews)
	if len(matched) == 0 {
		linkNames := make([]string, 0, len(links))
		for _, l := range links {
			linkNames = append(linkNames, l.Name)
		}
		s.logger.WarnContext(ctx, "no *arr indexer points at a linked Prowlarr; check the application's Prowlarr Server URL",
			slog.String("instance", inst.Name),
			slog.String("links", strings.Join(linkNames, ", ")),
		)
		return true, snatch.GateRefusal{}, nil
	}
	if hasUnmatched {
		return true, snatch.GateRefusal{}, nil
	}
	return s.checkExhaustion(ctx, matched, linkViews)
}

func matchSingleIndexer(bURL string, links []sqlcgen.ProwlarrLink, views map[uuid.UUID]LinkStatus) (matchedIndexer, bool) {
	for _, link := range links {
		prefixes := views[link.ID].MatchPrefixes
		if len(prefixes) == 0 {
			prefixes = []string{link.BaseUrl}
		}
		for _, prefix := range prefixes {
			if id, ok := arrclient.ProwlarrIndexerID(bURL, prefix); ok {
				return matchedIndexer{linkID: link.ID, linkName: link.Name, id: id}, true
			}
		}
	}
	return matchedIndexer{}, false
}

func matchIndexers(autoIndexers []arr.ProviderResource, links []sqlcgen.ProwlarrLink, views map[uuid.UUID]LinkStatus) ([]matchedIndexer, bool) {
	matched := make([]matchedIndexer, 0, len(autoIndexers))
	hasUnmatched := false
	for _, p := range autoIndexers {
		bURL := indexerBaseURL(p)
		if bURL == "" {
			hasUnmatched = true
			continue
		}
		m, ok := matchSingleIndexer(bURL, links, views)
		if ok {
			matched = append(matched, m)
		} else {
			hasUnmatched = true
		}
	}
	return matched, hasUnmatched
}

func evalIndexerStamina(idx IndexerStatus, dead bool, allLimitedExhausted *bool, hasAliveUnlimited *bool) {
	limited := (idx.QueryLimit != nil && *idx.QueryLimit > 0) || (idx.GrabLimit != nil && *idx.GrabLimit > 0)
	if limited {
		if !dead && !idx.Exhausted {
			*allLimitedExhausted = false
		}
	} else if !dead {
		*hasAliveUnlimited = true
	}
}

func (s *Service) checkExhaustion(ctx context.Context, matched []matchedIndexer, views map[uuid.UUID]LinkStatus) (bool, snatch.GateRefusal, error) {
	now := s.clk.Now()
	allLimitedExhaustedOrDead := true
	hasAliveUnlimited := false
	matchedKnownCount := 0
	anyGrabExhausted := false

	for _, m := range matched {
		view := views[m.linkID]
		if !view.Reachable {
			s.logger.WarnContext(ctx, "prowlarr unreachable during gate check, failing open", slog.String("link", m.linkName), slog.String("error", view.Error))
			return true, snatch.GateRefusal{}, nil
		}
		idx, found := findIndexer(view.Indexers, m.id)
		if !found {
			return true, snatch.GateRefusal{}, nil
		}
		matchedKnownCount++
		if idx.Exhausted && idx.ExhaustedBy == "grab_limit" {
			anyGrabExhausted = true
		}
		evalIndexerStamina(idx, isDead(idx, now), &allLimitedExhaustedOrDead, &hasAliveUnlimited)
	}
	if matchedKnownCount > 0 && allLimitedExhaustedOrDead && !hasAliveUnlimited {
		msg := "all Prowlarr indexers exhausted"
		if anyGrabExhausted {
			msg = "all Prowlarr indexers exhausted (grab limit)"
		}
		return false, snatch.GateRefusal{
			Code:    snatch.GateIndexerLimit,
			Message: msg,
		}, nil
	}
	return true, snatch.GateRefusal{}, nil
}

func isDead(idx IndexerStatus, now time.Time) bool {
	if !idx.Enabled {
		return true
	}
	return idx.DisabledTill != nil && idx.DisabledTill.After(now)
}

func indexerBaseURL(p arr.ProviderResource) string {
	for _, f := range p.Fields {
		if strings.EqualFold(f.Name, "baseUrl") {
			if s, ok := f.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func findIndexer(indexers []IndexerStatus, id int) (IndexerStatus, bool) {
	for _, idx := range indexers {
		if idx.ID == id {
			return idx, true
		}
	}
	return IndexerStatus{}, false
}

func normalize(in Input) (Input, error) {
	if in.HourlyCap == 0 {
		in.HourlyCap = domain.DefaultProwlarrHourlyCap
	}
	if err := domain.ValidateProwlarrLinkInput(in.Name, in.BaseURL, in.APIKey, in.HourlyCap); err != nil {
		return Input{}, fmt.Errorf("prowlarr: %w", err)
	}
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return Input{}, fmt.Errorf("prowlarr: %w", err)
	}
	in.BaseURL = normalized
	in.Name = strings.TrimSpace(in.Name)
	in.APIKey = strings.TrimSpace(in.APIKey)
	return in, nil
}
