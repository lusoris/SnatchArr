// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type stubProber struct {
	res arrclient.Result
	err error
}

func (p *stubProber) Probe(context.Context, domain.AppKind, string, string) (arrclient.Result, error) {
	return p.res, p.err
}

type stubSnapshotter struct{}

func (s *stubSnapshotter) Snapshot(context.Context, domain.DownloadClient, string) dlclients.Snapshot {
	return dlclients.Snapshot{Reachable: true}
}

type stubDiscoverer struct{}

func (d *stubDiscoverer) DownloadClients(context.Context, domain.AppKind, string, string) ([]arr.ProviderResource, error) {
	return nil, nil
}

type fixture struct {
	svc     *configarr.Service
	instSvc *instances.Service
	dlSvc   *dlclients.Service
	st      *store.Store
	clk     *clockwork.FakeClock
	probe   *stubProber
}

func newFixture(t *testing.T, opts ...func(*config.Options)) fixture {
	t.Helper()
	st := storetest.New(t)
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("newFixture: encryptor: %v", err)
	}
	clk := clock.NewFake()
	probe := &stubProber{res: arrclient.Result{AppName: "Sonarr", Version: "4.0.10.2544"}}
	logger := slog.New(slog.DiscardHandler)
	instSvc := instances.New(st, enc, clk, id.New(), probe, logger)
	dlSvc := dlclients.New(st, enc, clk, id.New(), &stubSnapshotter{}, &stubDiscoverer{}, instSvc, logger)
	cfg := config.Options{
		Profile: config.ProfileDev,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	svc := configarr.New(cfg, instSvc, dlSvc, probe, clk, logger)
	return fixture{
		svc:     svc,
		instSvc: instSvc,
		dlSvc:   dlSvc,
		st:      st,
		clk:     clk,
		probe:   probe,
	}
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	return p
}

func TestImport_UpdatesAndDisables(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
sonarr:
  a:
    base_url: http://sonarr-a.invalid:8989
    api_key: sonarr-key-a
radarr:
  b:
    base_url: http://radarr-b.invalid:7878
    api_key: radarr-key-b
`)

	idA, idB := testImportInitial(t, f, configPath)
	testImportSameAgain(t, f, configPath, idA, idB)
	testImportChangeA(t, f, dir, configPath, idA)
	testImportDropB(t, f, dir, configPath, idA, idB)
	testImportZeroInstances(t, f, dir, configPath, idA, idB)
}

func testImportInitial(t *testing.T, f fixture, configPath string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 2 || res.Updated != 0 || res.Disabled != 0 {
		t.Fatalf("initial Import() = %+v, %v; want Imported=2", res, err)
	}
	list, err := f.instSvc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("List() = len %d, %v; want 2", len(list), err)
	}
	var idA, idB uuid.UUID
	for _, inst := range list {
		if inst.Source != domain.SourceConfigarr || !inst.Enabled {
			t.Fatalf("instance %+v not enabled configarr source", inst)
		}
		if inst.Name == "a" && inst.BaseURL == "http://sonarr-a.invalid:8989" {
			idA = inst.ID
		}
		if inst.Name == "b" && inst.BaseURL == "http://radarr-b.invalid:7878" {
			idB = inst.ID
		}
	}
	if idA == uuid.Nil || idB == uuid.Nil {
		t.Fatalf("missing idA (%v) or idB (%v)", idA, idB)
	}
	return idA, idB
}

func testImportSameAgain(t *testing.T, f fixture, configPath string, idA, idB uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 0 || res.Updated != 2 || res.Disabled != 0 {
		t.Fatalf("same file Import() = %+v, %v; want Updated=2", res, err)
	}
	instA, err := f.instSvc.Get(ctx, idA)
	if err != nil || instA.ID != idA {
		t.Fatalf("instA changed ID or missing: %+v, %v", instA, err)
	}
	instB, err := f.instSvc.Get(ctx, idB)
	if err != nil || instB.ID != idB {
		t.Fatalf("instB changed ID or missing: %+v, %v", instB, err)
	}
}

func testImportChangeA(t *testing.T, f fixture, dir, configPath string, idA uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	writeConfig(t, dir, `
sonarr:
  a:
    base_url: http://sonarr-a-changed.invalid:8989
    api_key: sonarr-key-a
radarr:
  b:
    base_url: http://radarr-b.invalid:7878
    api_key: radarr-key-b
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 0 || res.Updated != 2 || res.Disabled != 0 {
		t.Fatalf("changed A Import() = %+v, %v; want Updated=2", res, err)
	}
	instA, err := f.instSvc.Get(ctx, idA)
	if err != nil || instA.ID != idA || instA.BaseURL != "http://sonarr-a-changed.invalid:8989" || instA.Name != "a" {
		t.Fatalf("instA after update: %+v, %v", instA, err)
	}
}

func testImportDropB(t *testing.T, f fixture, dir, configPath string, idA, idB uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	writeConfig(t, dir, `
sonarr:
  a:
    base_url: http://sonarr-a-changed.invalid:8989
    api_key: sonarr-key-a
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 0 || res.Updated != 1 || res.Disabled != 1 {
		t.Fatalf("drop B Import() = %+v, %v; want Updated=1, Disabled=1", res, err)
	}
	instB, err := f.instSvc.Get(ctx, idB)
	if err != nil || instB.Enabled {
		t.Fatalf("instB should be disabled: %+v, %v", instB, err)
	}
	instA, err := f.instSvc.Get(ctx, idA)
	if err != nil || !instA.Enabled {
		t.Fatalf("instA should remain enabled: %+v, %v", instA, err)
	}
}

func testImportZeroInstances(t *testing.T, f fixture, dir, configPath string, idA, idB uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	writeConfig(t, dir, `
trashGuideUrl: https://github.com/TRaSH-Guides/Guides
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 0 || res.Updated != 0 || res.Disabled != 1 {
		t.Fatalf("zero instances Import() = %+v, %v; want Disabled=1 (only a was active)", res, err)
	}
	instA, err := f.instSvc.Get(ctx, idA)
	if err != nil || instA.Enabled {
		t.Fatalf("instA should now be disabled: %+v, %v", instA, err)
	}
	instB, err := f.instSvc.Get(ctx, idB)
	if err != nil || instB.Enabled {
		t.Fatalf("instB should remain disabled: %+v, %v", instB, err)
	}
}

func TestImport_ConflictWithManualRow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	manualURL := "http://shared.invalid:8989"
	manualInst, err := f.instSvc.Create(ctx, instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "manual-sonarr",
		BaseURL: manualURL,
		APIKey:  "manual-key-1234",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
sonarr:
  conflicting:
    base_url: http://shared.invalid:8989
    api_key: conf-key-1
`)

	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil {
		t.Fatalf("Import() err = %v, want nil with warning", err)
	}
	if res.Imported != 0 || res.Updated != 0 {
		t.Fatalf("Import() result = %+v; want Imported=0, Updated=0", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "already configured by hand") {
		t.Fatalf("Import() warnings = %v, want conflict warning", res.Warnings)
	}

	got, err := f.instSvc.Get(ctx, manualInst.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.ID != manualInst.ID || got.Name != manualInst.Name || got.BaseURL != manualURL || got.Source != domain.SourceManual || !got.Enabled {
		t.Fatalf("manual instance was modified: %+v", got)
	}
	list, err := f.instSvc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("instances list: len=%d, want 1", len(list))
	}
}

func TestLinkedWatch_ReimportsOnFileChange(t *testing.T) {
	t.Parallel()
	t.Run("positive watch reimports on change", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		configPath := writeConfig(t, dir, "sonarr:\n  series:\n    base_url: http://sonarr:8989\n    api_key: sonarr-key-series-1\n")
		f := newFixture(t, func(o *config.Options) {
			o.Configarr.Config = configPath
			o.Configarr.Watch = true
		})
		testWatchPositive(t, f, dir)
	})
	t.Run("negative watch false skips reimport", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		configPath := writeConfig(t, dir, "sonarr:\n  series:\n    base_url: http://sonarr:8989\n    api_key: sonarr-key-series-1\n")
		f := newFixture(t, func(o *config.Options) {
			o.Configarr.Config = configPath
			o.Configarr.Watch = false
		})
		testWatchNegative(t, f, dir)
	})
}

func testWatchPositive(t *testing.T, f fixture, dir string) {
	t.Helper()
	ctx := t.Context()
	if err := f.svc.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.svc.Stop(stopCtx); err != nil {
			t.Errorf("Stop() = %v", err)
		}
	})

	if err := f.clk.BlockUntilContext(ctx, 1); err != nil {
		t.Fatalf("BlockUntilContext(1): %v", err)
	}
	list := waitInstancesCount(t, ctx, f.instSvc, 1)
	if len(list) != 1 || list[0].Name != "series" {
		t.Fatalf("initial import: %+v", list)
	}
	initialAt := waitImportAfter(t, f.svc, time.Time{})

	// Rewrite file with different size and additional instance.
	writeConfig(t, dir, `
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: sonarr-key-series-1
radarr:
  movies:
    base_url: http://radarr:7878
    api_key: radarr-key-movies-2
`)

	f.clk.Advance(configarr.PollInterval)
	list = waitInstancesCount(t, ctx, f.instSvc, 2)
	if len(list) != 2 {
		t.Fatalf("after advance: want 2 instances, got %d", len(list))
	}

	// The rows commit before Import records its time: wait for the record, not the rows.
	lastAt := waitImportAfter(t, f.svc, initialAt)

	// Advance clock again with no file change -> no extra import.
	f.clk.Advance(configarr.PollInterval)
	time.Sleep(50 * time.Millisecond)
	if st := f.svc.Status(); st.LastImportAt == nil || !st.LastImportAt.Equal(lastAt) {
		t.Fatalf("unexpected import without file change: at=%v, was=%v", st.LastImportAt, lastAt)
	}
	list = waitInstancesCount(t, ctx, f.instSvc, 2)
	if len(list) != 2 {
		t.Fatalf("instance count changed: %d", len(list))
	}
}

func testWatchNegative(t *testing.T, f fixture, dir string) {
	t.Helper()
	ctx := t.Context()
	if err := f.svc.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.svc.Stop(stopCtx); err != nil {
			t.Errorf("Stop() = %v", err)
		}
	})

	list := waitInstancesCount(t, ctx, f.instSvc, 1)
	if len(list) != 1 {
		t.Fatalf("initial import: %+v", list)
	}

	writeConfig(t, dir, `
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: sonarr-key-series-1
radarr:
  movies:
    base_url: http://radarr:7878
    api_key: radarr-key-movies-2
`)

	f.clk.Advance(configarr.PollInterval)
	time.Sleep(50 * time.Millisecond)

	list, err := f.instSvc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Watch=false re-imported unexpectedly: len=%d, %v", len(list), err)
	}
	st := f.svc.Status()
	if st.Watching {
		t.Fatalf("Status().Watching = true, want false")
	}
}

// waitPolls bounds the 10 ms polls of the waiters below to 10 s: they wait for an import to
// land, not for it to be fast, and under the race detector on a loaded host a first import
// into a fresh database has taken longer than 1 s.
const waitPolls = 1000

// waitImportAfter polls until the service records an import later than prev and returns its time.
func waitImportAfter(t *testing.T, svc *configarr.Service, prev time.Time) time.Time {
	t.Helper()
	for range waitPolls {
		if at := svc.Status().LastImportAt; at != nil && at.After(prev) {
			return *at
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no import recorded after %v", prev)
	return time.Time{}
}

func waitInstancesCount(t *testing.T, ctx context.Context, instSvc *instances.Service, want int) []domain.Instance {
	t.Helper()
	var list []domain.Instance
	for range waitPolls {
		var err error
		list, err = instSvc.List(ctx)
		if err == nil && len(list) == want {
			return list
		}
		time.Sleep(10 * time.Millisecond)
	}
	return list
}

func TestImport_OrphanedDeleteLifecycle(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
sonarr:
  a:
    base_url: http://sonarr-a.invalid:8989
    api_key: sonarr-key-a
`)

	ctx := t.Context()
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.Imported != 1 || res.Disabled != 0 {
		t.Fatalf("initial Import() = %+v, %v; want Imported=1", res, err)
	}
	list, err := f.instSvc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = len %d, %v; want 1", len(list), err)
	}
	idA := list[0].ID

	// Drop a from config -> Disabled=1.
	emptyConfig := writeConfig(t, dir, `
trashGuideUrl: https://github.com/TRaSH-Guides/Guides
`)
	res, err = f.svc.Import(ctx, emptyConfig, "")
	if err != nil || res.Disabled != 1 {
		t.Fatalf("drop a Import() = %+v, %v; want Disabled=1", res, err)
	}

	// Delete(a) -> nil.
	if err = f.instSvc.Delete(ctx, idA); err != nil {
		t.Fatalf("Delete(idA) = %v, want nil", err)
	}
	if _, getErr := f.instSvc.Get(ctx, idA); !errors.Is(getErr, domain.ErrNotFound) {
		t.Fatalf("Get(idA) = %v, want ErrNotFound", getErr)
	}

	// Import the same file again (without a) -> Imported=0, a not recreated.
	res, err = f.svc.Import(ctx, emptyConfig, "")
	if err != nil || res.Imported != 0 {
		t.Fatalf("re-import empty config = %+v, %v; want Imported=0", res, err)
	}
	list, err = f.instSvc.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("List() after re-import = len %d, %v; want 0", len(list), err)
	}

	// Plus: import file with a enabled: false -> Delete -> ErrForbidden.
	testImportDisabledRowRefusesDelete(t, f, dir)
}

func testImportDisabledRowRefusesDelete(t *testing.T, f fixture, dir string) {
	t.Helper()
	ctx := t.Context()
	disabledConfig := writeConfig(t, dir, `
sonarr:
  disabled_row:
    base_url: http://sonarr-disabled.invalid:8989
    api_key: sonarr-key-disabled
    enabled: false
`)
	res, err := f.svc.Import(ctx, disabledConfig, "")
	if err != nil || res.Imported != 1 {
		t.Fatalf("import enabled:false = %+v, %v; want Imported=1", res, err)
	}
	list, err := f.instSvc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() after enabled:false = len %d, %v; want 1", len(list), err)
	}
	idRow := list[0].ID
	err = f.instSvc.Delete(ctx, idRow)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Delete(enabled:false) = %v, want ErrForbidden", err)
	}
	if _, getErr := f.instSvc.Get(ctx, idRow); getErr != nil {
		t.Fatalf("Get(idRow) after refused delete = %v, want row to survive", getErr)
	}
}

func TestImport_DownloadClients(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
sonarr:
  main:
    base_url: http://sonarr-main.invalid:8989
    api_key: sonarr-key-main
`)
	dlClientsPath := writeFile(t, dir, "download-clients.yml", `
qbittorrent:
  main:
    base_url: http://qbit.media.svc:8080
    password: qbit-secret-pass
    instance: sonarr/main
    max_active: 5
    bandwidth_budget_bps: 10485760
    enabled: true
sabnzbd:
  main:
    base_url: http://sabnzbd.media.svc:8080
    api_key: sab-api-key
`)

	f := newFixture(t, func(o *config.Options) {
		o.Configarr.Config = configPath
		o.Configarr.DownloadClients = dlClientsPath
	})

	qbitID, sabID := testDownloadClientsInitial(t, f, configPath)
	testDownloadClientsSecondImport(t, f, configPath, dir, qbitID)
	testDownloadClientsPrune(t, f, configPath, dir, qbitID, sabID)
	testDownloadClientsForbidden(t, f, qbitID)
	testDownloadClientsInvalidWarning(t, f, configPath, dir)
}

func testDownloadClientsInitial(t *testing.T, f fixture, configPath string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.ClientsImported != 2 || res.ClientsUpdated != 0 || res.ClientsRemoved != 0 {
		t.Fatalf("initial Import() clients = %+v, %v; want ClientsImported=2", res, err)
	}
	list, err := f.dlSvc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("dlSvc.List() = len %d, %v; want 2", len(list), err)
	}
	var qbitID, sabID uuid.UUID
	for _, c := range list {
		if c.Source != domain.ClientSourceConfigarr {
			t.Fatalf("client %+v source = %q, want configarr", c, c.Source)
		}
		if c.Name == "main" && c.Kind == domain.ClientQBittorrent {
			qbitID = c.ID
			if c.InstanceID == nil {
				t.Fatalf("expected qbit to link to sonarr instance")
			}
			if c.MaxActive != 5 || c.BandwidthBudgetBPS != 10485760 {
				t.Fatalf("qbit limits mismatch: %+v", c)
			}
		}
		if c.Name == "main" && c.Kind == domain.ClientSABnzbd {
			sabID = c.ID
			if c.InstanceID != nil {
				t.Fatalf("expected sabnzbd to have nil instanceID")
			}
		}
	}
	if qbitID == uuid.Nil || sabID == uuid.Nil {
		t.Fatalf("missing qbitID (%v) or sabID (%v)", qbitID, sabID)
	}
	return qbitID, sabID
}

func testDownloadClientsSecondImport(t *testing.T, f fixture, configPath, dir string, qbitID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.ClientsImported != 0 || res.ClientsUpdated != 2 || res.ClientsRemoved != 0 {
		t.Fatalf("second Import() = %+v, %v; want ClientsUpdated=2", res, err)
	}
	writeFile(t, dir, "download-clients.yml", `
qbittorrent:
  main:
    base_url: http://qbit.media.svc:8080
    password: qbit-secret-pass
    instance: sonarr/main
    max_active: 10
sabnzbd:
  main:
    base_url: http://sabnzbd.media.svc:8080
    api_key: sab-api-key
`)
	res, err = f.svc.Import(ctx, configPath, "")
	if err != nil || res.ClientsUpdated != 2 {
		t.Fatalf("third Import() after update = %+v, %v; want ClientsUpdated=2", res, err)
	}
	qbit, err := f.dlSvc.Get(ctx, qbitID)
	if err != nil || qbit.MaxActive != 10 {
		t.Fatalf("qbit MaxActive = %d, %v; want 10", qbit.MaxActive, err)
	}
}

func testDownloadClientsPrune(t *testing.T, f fixture, configPath, dir string, qbitID, sabID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	writeFile(t, dir, "download-clients.yml", `
qbittorrent:
  main:
    base_url: http://qbit.media.svc:8080
    password: qbit-secret-pass
    instance: sonarr/main
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil || res.ClientsRemoved != 1 {
		t.Fatalf("import after drop = %+v, %v; want ClientsRemoved=1", res, err)
	}
	list, err := f.dlSvc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() after drop = len %d, %v; want 1", len(list), err)
	}
	if list[0].ID != qbitID {
		t.Fatalf("surviving client ID = %v, want %v", list[0].ID, qbitID)
	}
	if _, getErr := f.dlSvc.Get(ctx, sabID); !errors.Is(getErr, domain.ErrNotFound) {
		t.Fatalf("Get(sabID) = %v, want ErrNotFound", getErr)
	}
}

func testDownloadClientsForbidden(t *testing.T, f fixture, qbitID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	_, upErr := f.dlSvc.Update(ctx, qbitID, dlclients.Input{
		Kind:    domain.ClientQBittorrent,
		Name:    "new-name",
		BaseURL: "http://qbit-new:8080",
	})
	if !errors.Is(upErr, domain.ErrForbidden) {
		t.Fatalf("Update configarr client err = %v, want ErrForbidden", upErr)
	}
	if !strings.Contains(upErr.Error(), "download client is managed by Configarr; edit the download clients file instead") {
		t.Fatalf("Update err text = %q, want managed by Configarr", upErr.Error())
	}

	delErr := f.dlSvc.Delete(ctx, qbitID)
	if !errors.Is(delErr, domain.ErrForbidden) {
		t.Fatalf("Delete configarr client err = %v, want ErrForbidden", delErr)
	}
	if !strings.Contains(delErr.Error(), "download client is managed by Configarr; edit the download clients file instead") {
		t.Fatalf("Delete err text = %q, want managed by Configarr", delErr.Error())
	}
}

func testDownloadClientsInvalidWarning(t *testing.T, f fixture, configPath, dir string) {
	t.Helper()
	ctx := t.Context()
	writeFile(t, dir, "download-clients.yml", `
qbittorrent:
  "   ":
    base_url: http://qbit.media.svc:8080
    password: qbit-secret-pass
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil {
		t.Fatalf("Import with invalid client err = %v, want nil", err)
	}
	if res.ClientsImported != 0 {
		t.Fatalf("ClientsImported = %d, want 0", res.ClientsImported)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("want warning for empty-after-trim name, got none")
	}
	var found bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "name must be 1-64 characters") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning about name, got %v", res.Warnings)
	}
	list, err := f.dlSvc.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("dlSvc.List() = len %d, %v; want 0", len(list), err)
	}
}
