// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Input is the user-supplied part of a download client.
type Input struct {
	InstanceID         *uuid.UUID
	Kind               domain.DownloadClientKind
	Name               string
	BaseURL            string
	Username           string
	Secret             string // password, or API key for SABnzbd
	Enabled            bool
	MaxActive          int
	BandwidthBudgetBPS int64
}

// DiscoveryResult summarises one import from an *arr's download-client list.
type DiscoveryResult struct {
	Imported    int
	Updated     int
	Removed     int
	Unsupported []string
	Warnings    []string
}

// SnapshotTTL is how long an observation is reused before the client is asked again.
const SnapshotTTL = 20 * time.Second

// unchangedSecret marks "keep the stored secret" on update.
const unchangedSecret = "<unchanged>"

// Service is the download-client use-case layer.
type Service struct {
	st        *store.Store
	enc       *crypto.Encryptor
	clk       clock.Clock
	ids       id.Generator
	observer  Snapshotter
	discover  arrclient.Discoverer
	instances *instances.Service
	logger    *slog.Logger

	mu    sync.Mutex
	cache map[uuid.UUID]Snapshot
}

// New wires the service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator, observer Snapshotter,
	discover arrclient.Discoverer, inst *instances.Service, logger *slog.Logger,
) *Service {
	return &Service{
		st: st, enc: enc, clk: clk, ids: ids, observer: observer, discover: discover, instances: inst, logger: logger,
		cache: make(map[uuid.UUID]Snapshot),
	}
}

// List returns every download client without secrets.
func (s *Service) List(ctx context.Context) ([]domain.DownloadClient, error) {
	rows, err := s.st.Q().ListDownloadClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("dlclients: list: %w", store.MapError(err))
	}
	return store.DownloadClientsFromRows(rows), nil
}

// Get returns one download client without its secret.
func (s *Service) Get(ctx context.Context, clientID uuid.UUID) (domain.DownloadClient, error) {
	row, err := s.st.Q().GetDownloadClient(ctx, clientID)
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: get: %w", store.MapError(err))
	}
	return store.DownloadClientFromRow(row), nil
}

// Create validates, encrypts and stores a manual download client.
func (s *Service) Create(ctx context.Context, in Input) (domain.DownloadClient, error) {
	in, err := s.normalize(ctx, in)
	if err != nil {
		return domain.DownloadClient{}, err
	}
	clientID, err := s.ids.NewUUID()
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.Secret))
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: seal secret: %w", err)
	}
	row, err := s.st.Q().CreateDownloadClient(ctx, sqlcgen.CreateDownloadClientParams{
		ID: clientID, InstanceID: in.InstanceID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL,
		Username: in.Username, SecretEnc: sealed, Enabled: in.Enabled, Source: string(domain.ClientSourceManual),
		MaxActive:          int32(in.MaxActive), // #nosec G115 -- validated 0..1000
		BandwidthBudgetBps: in.BandwidthBudgetBPS,
		CreatedAt:          s.clk.Now(),
	})
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: create: %w", store.MapError(err))
	}
	return store.DownloadClientFromRow(row), nil
}

// Update replaces the editable fields. An empty Secret keeps the stored one.
func (s *Service) Update(ctx context.Context, clientID uuid.UUID, in Input) (domain.DownloadClient, error) {
	current, err := s.st.Q().GetDownloadClient(ctx, clientID)
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: get: %w", store.MapError(err))
	}
	if in.Secret == "" {
		in.Secret = unchangedSecret
	}
	in, err = s.normalize(ctx, in)
	if err != nil {
		return domain.DownloadClient{}, err
	}
	sealed := current.SecretEnc
	if in.Secret != unchangedSecret {
		if sealed, err = s.enc.Seal([]byte(in.Secret)); err != nil {
			return domain.DownloadClient{}, fmt.Errorf("dlclients: seal secret: %w", err)
		}
	}
	row, err := s.st.Q().UpdateDownloadClient(ctx, sqlcgen.UpdateDownloadClientParams{
		ID: clientID, InstanceID: in.InstanceID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL,
		Username: in.Username, SecretEnc: sealed, Enabled: in.Enabled,
		MaxActive:          int32(in.MaxActive), // #nosec G115 -- validated 0..1000
		BandwidthBudgetBps: in.BandwidthBudgetBPS,
		UpdatedAt:          s.clk.Now(),
	})
	if err != nil {
		return domain.DownloadClient{}, fmt.Errorf("dlclients: update: %w", store.MapError(err))
	}
	s.forget(clientID)
	return store.DownloadClientFromRow(row), nil
}

// Delete removes a download client.
func (s *Service) Delete(ctx context.Context, clientID uuid.UUID) error {
	n, err := s.st.Q().DeleteDownloadClient(ctx, clientID)
	if err != nil {
		return fmt.Errorf("dlclients: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("dlclients: delete: %w", domain.ErrNotFound)
	}
	s.forget(clientID)
	return nil
}

// Test observes a stored client right now, records the outcome and refreshes the cache.
func (s *Service) Test(ctx context.Context, clientID uuid.UUID) (Snapshot, error) {
	row, err := s.st.Q().GetDownloadClient(ctx, clientID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("dlclients: get: %w", store.MapError(err))
	}
	return s.observe(ctx, store.DownloadClientFromRow(row), row.SecretEnc), nil
}

// TestInput observes credentials that are not stored yet.
func (s *Service) TestInput(ctx context.Context, in Input) (Snapshot, error) {
	in, err := s.normalize(ctx, in)
	if err != nil {
		return Snapshot{}, err
	}
	c := domain.DownloadClient{Kind: in.Kind, Name: in.Name, BaseURL: in.BaseURL, Username: in.Username, Enabled: true}
	return s.observer.Snapshot(ctx, c, in.Secret), nil
}

// Status returns a (cached) observation of every enabled client, for the dashboard.
func (s *Service) Status(ctx context.Context) ([]Snapshot, error) {
	rows, err := s.st.Q().ListEnabledDownloadClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("dlclients: status: %w", store.MapError(err))
	}
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.cachedObserve(ctx, row))
	}
	return out, nil
}

// Verdict evaluates backpressure for one instance over its own and the global clients.
func (s *Service) Verdict(ctx context.Context, instanceID uuid.UUID) (Verdict, error) {
	rows, err := s.st.Q().ListDownloadClientsForInstance(ctx, &instanceID)
	if err != nil {
		return Verdict{}, fmt.Errorf("dlclients: verdict: %w", store.MapError(err))
	}
	clients := make([]domain.DownloadClient, 0, len(rows))
	snaps := make(map[uuid.UUID]Snapshot, len(rows))
	for _, row := range rows {
		clients = append(clients, store.DownloadClientFromRow(row))
		snaps[row.ID] = s.cachedObserve(ctx, row)
	}
	return Evaluate(clients, snaps), nil
}

// PaceFactor is the bandwidth share a leased run may use (workergrpc.Pacer).
func (s *Service) PaceFactor(ctx context.Context, instanceID uuid.UUID) (float64, error) {
	v, err := s.Verdict(ctx, instanceID)
	if err != nil {
		return 1, err
	}
	if !v.Allowed {
		return 0, nil
	}
	return v.PaceFactor, nil
}

func (s *Service) cachedObserve(ctx context.Context, row sqlcgen.DownloadClient) Snapshot {
	s.mu.Lock()
	snap, ok := s.cache[row.ID]
	s.mu.Unlock()
	if ok && s.clk.Now().Before(snap.CheckedAt.Add(SnapshotTTL)) {
		return snap
	}
	return s.observe(ctx, store.DownloadClientFromRow(row), row.SecretEnc)
}

func (s *Service) observe(ctx context.Context, c domain.DownloadClient, sealed []byte) Snapshot {
	secret, err := s.enc.Open(sealed)
	if err != nil {
		return Snapshot{ClientID: c.ID, Name: c.Name, Kind: c.Kind, CheckedAt: s.clk.Now(), Error: "stored secret cannot be decrypted"}
	}
	snap := s.observer.Snapshot(ctx, c, string(secret))
	s.mu.Lock()
	s.cache[c.ID] = snap
	s.mu.Unlock()
	s.recordCheck(ctx, c.ID, snap)
	return snap
}

func (s *Service) recordCheck(ctx context.Context, clientID uuid.UUID, snap Snapshot) {
	var lastErr *string
	if !snap.Reachable {
		msg := snap.Error
		lastErr = &msg
	}
	err := s.st.Q().RecordDownloadClientCheck(ctx, sqlcgen.RecordDownloadClientCheckParams{
		ID: clientID, LastCheckAt: &snap.CheckedAt, LastError: lastErr,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "dlclients: record check", slog.String("error", err.Error()))
	}
}

func (s *Service) forget(clientID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, clientID)
	s.mu.Unlock()
}

func (s *Service) normalize(ctx context.Context, in Input) (Input, error) {
	if err := domain.ValidateDownloadClientInput(in.Kind, in.Name, in.BaseURL, in.MaxActive, in.BandwidthBudgetBPS); err != nil {
		return Input{}, fmt.Errorf("dlclients: %w", err)
	}
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return Input{}, fmt.Errorf("dlclients: %w", err)
	}
	if in.InstanceID != nil {
		if _, err := s.instances.Get(ctx, *in.InstanceID); err != nil {
			return Input{}, fmt.Errorf("dlclients: instance: %w", err)
		}
	}
	in.BaseURL = normalized
	in.Name = strings.TrimSpace(in.Name)
	in.Username = strings.TrimSpace(in.Username)
	return in, nil
}

// Discover imports the download clients an *arr instance has configured, updating the
// ones seen before and removing discovered ones the instance no longer lists.
func (s *Service) Discover(ctx context.Context, instanceID uuid.UUID) (DiscoveryResult, error) {
	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return DiscoveryResult{}, err
	}
	creds, err := s.instances.Credentials(ctx, instanceID)
	if err != nil {
		return DiscoveryResult{}, err
	}
	providers, err := s.discover.DownloadClients(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("dlclients: discover: %w", err)
	}
	var res DiscoveryResult
	keep := make([]int32, 0, len(providers))
	for _, p := range providers {
		d, ok := FromProvider(instanceID, p)
		if !ok {
			res.Unsupported = append(res.Unsupported, fmt.Sprintf("%s (%s)", p.Name, p.ImplementationName))
			continue
		}
		if _, urlErr := domain.NormalizeBaseURL(d.Input.BaseURL); urlErr != nil {
			res.Warnings = append(res.Warnings, d.Input.Name+": "+urlErr.Error())
			continue
		}
		if upErr := s.upsertDiscovered(ctx, d, &res); upErr != nil {
			return res, upErr
		}
		keep = append(keep, int32(d.RemoteID)) // #nosec G115 -- *arr ids are small integers
	}
	removed, err := s.st.Q().DeleteStaleDiscoveredDownloadClients(ctx, sqlcgen.DeleteStaleDiscoveredDownloadClientsParams{InstanceID: &instanceID, KeepRemoteIds: keep})
	if err != nil {
		return res, fmt.Errorf("dlclients: prune: %w", store.MapError(err))
	}
	res.Removed = int(removed)
	return res, nil
}

func (s *Service) upsertDiscovered(ctx context.Context, d Discovered, res *DiscoveryResult) error {
	in := d.Input
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return fmt.Errorf("dlclients: %w", err)
	}
	if d.RedactedSecret {
		res.Warnings = append(res.Warnings, in.Name+": the *arr masks its secret; enter it by hand")
	}
	clientID, err := s.ids.NewUUID()
	if err != nil {
		return fmt.Errorf("dlclients: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.Secret))
	if err != nil {
		return fmt.Errorf("dlclients: seal secret: %w", err)
	}
	remote := int32(d.RemoteID) // #nosec G115 -- *arr ids are small integers
	row, err := s.st.Q().UpsertDiscoveredDownloadClient(ctx, sqlcgen.UpsertDiscoveredDownloadClientParams{
		ID: clientID, InstanceID: in.InstanceID, Kind: string(in.Kind), Name: strings.TrimSpace(in.Name), BaseUrl: normalized,
		Username: strings.TrimSpace(in.Username), SecretEnc: sealed, Enabled: in.Enabled, RemoteID: &remote, CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("dlclients: upsert discovered: %w", store.MapError(err))
	}
	if row.Inserted {
		res.Imported++
	} else {
		res.Updated++
	}
	s.forget(row.ID)
	return nil
}
