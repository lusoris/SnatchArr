// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func seedTestDownloadClient(t *testing.T, f *integrationFixture) domain.DownloadClient {
	t.Helper()
	c, err := f.h.clients.Create(t.Context(), dlclients.Input{
		Kind:      domain.ClientQBittorrent,
		Name:      "qbit-" + uuid.NewString()[:8],
		BaseURL:   "http://qbit-" + uuid.NewString()[:8] + ".invalid:8080",
		Username:  "admin",
		Secret:    "secret-pass",
		Enabled:   true,
		MaxActive: 10,
	})
	if err != nil {
		t.Fatalf("seedTestDownloadClient: %v", err)
	}
	return c
}

func validDownloadClientInput() oas.DownloadClientInput {
	return oas.DownloadClientInput{
		Kind:               oas.DownloadClientKindQbittorrent,
		Name:               "qbit-test",
		BaseURL:            "http://qbit.invalid:8080",
		Username:           oas.NewOptString("admin"),
		Secret:             oas.NewOptString("secret-pass"),
		Enabled:            oas.NewOptBool(true),
		MaxActive:          oas.NewOptInt(10),
		BandwidthBudgetBps: oas.NewOptInt64(1048576),
	}
}

func writeConfigarrYAML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeConfigarrYAML: %v", err)
	}
	return p
}

func assertListedClientsMatch(t *testing.T, list []oas.DownloadClient, c1, c2 domain.DownloadClient) {
	t.Helper()
	byID := make(map[uuid.UUID]oas.DownloadClient, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	item1, ok1 := byID[c1.ID]
	item2, ok2 := byID[c2.ID]
	if !ok1 || !ok2 {
		t.Fatalf("ListDownloadClients() missing seeded clients: %v, %v", ok1, ok2)
	}
	if item1.Name != c1.Name || item1.Kind != oas.DownloadClientKind(c1.Kind) || item1.Protocol != oas.DownloadClientProtocolTorrent {
		t.Fatalf("ListDownloadClients() item1 = %+v, want %+v", item1, c1)
	}
	if item2.Name != c2.Name || item2.Kind != oas.DownloadClientKind(c2.Kind) || item2.BaseURL != c2.BaseURL {
		t.Fatalf("ListDownloadClients() item2 = %+v, want %+v", item2, c2)
	}
}

func TestDownloadClientsListAndGet(t *testing.T) {
	t.Parallel()
	t.Run("positive list download clients", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		c1 := seedTestDownloadClient(t, f)
		c2 := seedTestDownloadClient(t, f)
		list, err := f.h.ListDownloadClients(withUser(t.Context(), f.admin))
		if err != nil || len(list) != 2 {
			t.Fatalf("ListDownloadClients() = %v, len=%d, want 2", err, len(list))
		}
		assertListedClientsMatch(t, list, c1, c2)
	})
	t.Run("boundary list download clients empty", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		list, err := f.h.ListDownloadClients(withUser(t.Context(), f.admin))
		if err != nil || len(list) != 0 {
			t.Fatalf("ListDownloadClients() empty = %v, len=%d, want 0", err, len(list))
		}
	})
	t.Run("positive get download client", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		c := seedTestDownloadClient(t, f)
		got, err := f.h.GetDownloadClient(withUser(t.Context(), f.admin), oas.GetDownloadClientParams{ClientId: c.ID})
		if err != nil || got.ID != c.ID || got.Name != c.Name || got.Kind != oas.DownloadClientKind(c.Kind) {
			t.Fatalf("GetDownloadClient() = %+v, %v; want ID %s", got, err, c.ID)
		}
		if got.Protocol != oas.DownloadClientProtocolTorrent || got.BaseURL != c.BaseURL {
			t.Fatalf("GetDownloadClient() = %+v, want protocol torrent and baseURL %s", got, c.BaseURL)
		}
	})
	t.Run("negative get download client not found", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.GetDownloadClient(withUser(t.Context(), f.admin), oas.GetDownloadClientParams{ClientId: uuid.New()})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("GetDownloadClient() err = %v, want ErrNotFound", err)
		}
	})
}

func execCreateTestCase(t *testing.T, f *integrationFixture, in oas.DownloadClientInput, ctx context.Context, wantErr error) {
	t.Helper()
	got, err := f.h.CreateDownloadClient(ctx, &in)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("CreateDownloadClient() err = %v, want %v", err, wantErr)
		}
		list, listErr := f.h.ListDownloadClients(ctx)
		if listErr != nil || len(list) != 0 {
			t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
		}
		return
	}
	if err != nil || got.ID == uuid.Nil || got.Name != in.Name || got.Kind != in.Kind || got.BaseURL != in.BaseURL {
		t.Fatalf("CreateDownloadClient() = %+v, %v; want name %s", got, err, in.Name)
	}
	list, listErr := f.h.ListDownloadClients(ctx)
	if listErr != nil || len(list) != 1 {
		t.Fatalf("ListDownloadClients() after create = %d, want 1", len(list))
	}
}

func TestCreateDownloadClient(t *testing.T) {
	t.Parallel()
	emptyName := validDownloadClientInput()
	emptyName.Name = ""
	zeroMax := validDownloadClientInput()
	zeroMax.MaxActive = oas.NewOptInt(0)
	maxCap := validDownloadClientInput()
	maxCap.MaxActive = oas.NewOptInt(1000)
	overCap := validDownloadClientInput()
	overCap.MaxActive = oas.NewOptInt(1001)

	cases := []struct {
		name    string
		user    *domain.User
		in      oas.DownloadClientInput
		wantErr error
	}{
		{"positive admin creates download client", &domain.User{Role: domain.RoleAdmin}, validDownloadClientInput(), nil},
		{"negative no user", nil, validDownloadClientInput(), domain.ErrUnauthorized},
		{"negative viewer forbidden", &domain.User{Role: domain.RoleViewer}, validDownloadClientInput(), domain.ErrForbidden},
		{"negative invalid empty name", &domain.User{Role: domain.RoleAdmin}, emptyName, domain.ErrInvalid},
		{"boundary max active zero", &domain.User{Role: domain.RoleAdmin}, zeroMax, nil},
		{"boundary max active max limit", &domain.User{Role: domain.RoleAdmin}, maxCap, nil},
		{"boundary max active above limit", &domain.User{Role: domain.RoleAdmin}, overCap, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			execCreateTestCase(t, f, tc.in, ctx, tc.wantErr)
		})
	}
}

func execUpdateTestCase(t *testing.T, f *integrationFixture, targetID uuid.UUID, orig domain.DownloadClient, in oas.DownloadClientInput, ctx context.Context, wantErr error) {
	t.Helper()
	got, err := f.h.UpdateDownloadClient(ctx, &in, oas.UpdateDownloadClientParams{ClientId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateDownloadClient() err = %v, want %v", err, wantErr)
		}
		c, getErr := f.h.GetDownloadClient(ctx, oas.GetDownloadClientParams{ClientId: orig.ID})
		if getErr != nil || c.Name != orig.Name {
			t.Fatalf("after refused update: name = %q, want %q (err=%v)", c.Name, orig.Name, getErr)
		}
		return
	}
	if err != nil || got.Name != in.Name || got.MaxActive != in.MaxActive.Or(0) {
		t.Fatalf("UpdateDownloadClient() = %+v, %v; want name %s", got, err, in.Name)
	}
	c, getErr := f.h.GetDownloadClient(ctx, oas.GetDownloadClientParams{ClientId: orig.ID})
	if getErr != nil || c.Name != in.Name {
		t.Fatalf("after successful update: name = %q, want %q", c.Name, in.Name)
	}
}

func TestUpdateDownloadClient(t *testing.T) {
	t.Parallel()
	validUp := validDownloadClientInput()
	validUp.Name = "qbit-updated"
	validUp.MaxActive = oas.NewOptInt(5)
	emptyName := validUp
	emptyName.Name = ""
	zeroMax := validUp
	zeroMax.MaxActive = oas.NewOptInt(0)

	cases := []struct {
		name       string
		user       *domain.User
		useUnknown bool
		in         oas.DownloadClientInput
		wantErr    error
	}{
		{"positive update download client", &domain.User{Role: domain.RoleAdmin}, false, validUp, nil},
		{"negative update no user", nil, false, validUp, domain.ErrUnauthorized},
		{"negative update viewer", &domain.User{Role: domain.RoleViewer}, false, validUp, domain.ErrForbidden},
		{"negative update not found", &domain.User{Role: domain.RoleAdmin}, true, validUp, domain.ErrNotFound},
		{"negative update invalid empty name", &domain.User{Role: domain.RoleAdmin}, false, emptyName, domain.ErrInvalid},
		{"boundary update max active zero", &domain.User{Role: domain.RoleAdmin}, false, zeroMax, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			orig := seedTestDownloadClient(t, f)
			targetID := orig.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			execUpdateTestCase(t, f, targetID, orig, tc.in, ctx, tc.wantErr)
		})
	}
	t.Run("positive update omit secret keeps stored", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		orig := seedTestDownloadClient(t, f)
		omitSecret := validUp
		omitSecret.Secret = oas.OptString{}
		adminCtx := withUser(t.Context(), f.admin)
		got, err := f.h.UpdateDownloadClient(adminCtx, &omitSecret, oas.UpdateDownloadClientParams{ClientId: orig.ID})
		if err != nil || got.Name != omitSecret.Name {
			t.Fatalf("UpdateDownloadClient() omit secret = %+v, %v", got, err)
		}
		snap, testErr := f.h.TestDownloadClient(adminCtx, oas.TestDownloadClientParams{ClientId: orig.ID})
		if testErr != nil || !snap.Reachable {
			t.Fatalf("TestDownloadClient() after omit secret update = %+v, %v", snap, testErr)
		}
	})
}

func execDeleteTestCase(t *testing.T, f *integrationFixture, targetID uuid.UUID, orig domain.DownloadClient, ctx context.Context, wantErr error) {
	t.Helper()
	err := f.h.DeleteDownloadClient(ctx, oas.DeleteDownloadClientParams{ClientId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("DeleteDownloadClient() err = %v, want %v", err, wantErr)
		}
		if _, getErr := f.h.GetDownloadClient(ctx, oas.GetDownloadClientParams{ClientId: orig.ID}); getErr != nil {
			t.Fatalf("original client lost after refused delete: %v", getErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("DeleteDownloadClient() = %v", err)
	}
	if _, getErr := f.h.GetDownloadClient(ctx, oas.GetDownloadClientParams{ClientId: targetID}); !errors.Is(getErr, domain.ErrNotFound) {
		t.Fatalf("client still found after delete: %v", getErr)
	}
}

func TestDeleteDownloadClient(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		user       *domain.User
		useUnknown bool
		wantErr    error
	}{
		{"positive delete download client", &domain.User{Role: domain.RoleAdmin}, false, nil},
		{"negative delete no user", nil, false, domain.ErrUnauthorized},
		{"negative delete viewer", &domain.User{Role: domain.RoleViewer}, false, domain.ErrForbidden},
		{"negative delete not found", &domain.User{Role: domain.RoleAdmin}, true, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			orig := seedTestDownloadClient(t, f)
			targetID := orig.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			execDeleteTestCase(t, f, targetID, orig, ctx, tc.wantErr)
		})
	}
}

func TestDownloadClientTesting(t *testing.T) {
	t.Parallel()
	t.Run("positive test stored client", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		c := seedTestDownloadClient(t, f)
		got, err := f.h.TestDownloadClient(t.Context(), oas.TestDownloadClientParams{ClientId: c.ID})
		if err != nil || got.ClientID.Value != c.ID || !got.Reachable || got.Version.Value != "4.5.2" {
			t.Fatalf("TestDownloadClient() = %+v, %v; want ID %s", got, err, c.ID)
		}
	})
	t.Run("negative test stored client not found", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.TestDownloadClient(t.Context(), oas.TestDownloadClientParams{ClientId: uuid.New()})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("TestDownloadClient() err = %v, want ErrNotFound", err)
		}
	})
	testClientInputCases(t)
}

func testClientInputCases(t *testing.T) {
	t.Helper()
	validIn := validDownloadClientInput()
	badURL := validIn
	badURL.BaseURL = "://invalid-url"
	emptyNm := validIn
	emptyNm.Name = ""

	cases := []struct {
		name    string
		user    *domain.User
		in      oas.DownloadClientInput
		wantErr error
	}{
		{"positive test client input", &domain.User{Role: domain.RoleAdmin}, validIn, nil},
		{"negative test client input no user", nil, validIn, domain.ErrUnauthorized},
		{"negative test client input viewer", &domain.User{Role: domain.RoleViewer}, validIn, domain.ErrForbidden},
		{"negative test client input invalid url", &domain.User{Role: domain.RoleAdmin}, badURL, domain.ErrInvalid},
		{"boundary test client input empty name", &domain.User{Role: domain.RoleAdmin}, emptyNm, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := f.h.TestDownloadClientInput(ctx, &tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("TestDownloadClientInput() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || !got.Reachable || got.Name != tc.in.Name || got.Kind != tc.in.Kind {
				t.Fatalf("TestDownloadClientInput() = %+v, %v; want name %s", got, err, tc.in.Name)
			}
		})
	}
}

func TestGetDownloadClientStatus(t *testing.T) {
	t.Parallel()
	t.Run("positive status enabled client", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		c := seedTestDownloadClient(t, f)
		list, err := f.h.GetDownloadClientStatus(t.Context())
		if err != nil || len(list) != 1 {
			t.Fatalf("GetDownloadClientStatus() = %v, len=%d, want 1", err, len(list))
		}
		item := list[0]
		if item.ClientID.Value != c.ID || item.Name != c.Name || item.Kind != oas.DownloadClientKind(c.Kind) || !item.Reachable {
			t.Fatalf("GetDownloadClientStatus() item = %+v, want ID %s", item, c.ID)
		}
	})
	t.Run("boundary status no enabled clients", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		list, err := f.h.GetDownloadClientStatus(t.Context())
		if err != nil || len(list) != 0 {
			t.Fatalf("GetDownloadClientStatus() empty = %v, len=%d, want 0", err, len(list))
		}
	})
}

func TestDiscoverDownloadClients(t *testing.T) {
	t.Parallel()
	t.Run("positive discover download clients", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		enabled := true
		f.dlDisc.res = []arr.ProviderResource{
			{
				ID: 101, Name: "qbit-disc", Implementation: "QBittorrent", ImplementationName: "qBittorrent", Enable: &enabled,
				Fields: []arr.ProviderField{
					{Name: "host", Value: "127.0.0.1"},
					{Name: "port", Value: float64(8080)},
					{Name: "username", Value: "admin"},
					{Name: "password", Value: "secret"},
				},
			},
			{
				ID: 102, Name: "flood-disc", Implementation: "Flood", ImplementationName: "Flood", Enable: &enabled,
			},
		}
		adminCtx := withUser(t.Context(), f.admin)
		res, err := f.h.DiscoverDownloadClients(adminCtx, oas.DiscoverDownloadClientsParams{InstanceId: instID})
		if err != nil || res.Imported != 1 || len(res.Unsupported) != 1 || res.Unsupported[0] != "flood-disc (Flood)" {
			t.Fatalf("DiscoverDownloadClients() = %+v, %v; want 1 imported, 1 unsupported", res, err)
		}
		list, listErr := f.h.ListDownloadClients(adminCtx)
		if listErr != nil || len(list) != 1 || list[0].Name != "qbit-disc" {
			t.Fatalf("after discover: len = %d, item = %+v, %v", len(list), list, listErr)
		}
	})
	t.Run("negative discover no user", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		_, err := f.h.DiscoverDownloadClients(t.Context(), oas.DiscoverDownloadClientsParams{InstanceId: instID})
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("DiscoverDownloadClients() err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("negative discover viewer", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		_, err := f.h.DiscoverDownloadClients(withUser(t.Context(), f.viewer), oas.DiscoverDownloadClientsParams{InstanceId: instID})
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("DiscoverDownloadClients() err = %v, want ErrForbidden", err)
		}
	})
	t.Run("negative discover unknown instance", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		adminCtx := withUser(t.Context(), f.admin)
		_, err := f.h.DiscoverDownloadClients(adminCtx, oas.DiscoverDownloadClientsParams{InstanceId: uuid.New()})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("DiscoverDownloadClients() err = %v, want ErrNotFound", err)
		}
	})
	t.Run("boundary discover empty list", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		f.dlDisc.res = nil
		adminCtx := withUser(t.Context(), f.admin)
		res, err := f.h.DiscoverDownloadClients(adminCtx, oas.DiscoverDownloadClientsParams{InstanceId: instID})
		if err != nil || res.Imported != 0 || res.Updated != 0 || len(res.Unsupported) != 0 {
			t.Fatalf("DiscoverDownloadClients() empty = %+v, %v", res, err)
		}
	})
}

func testConfigarrImportPositiveAndStatus(t *testing.T, f *integrationFixture, adminCtx context.Context) {
	t.Helper()
	dir := t.TempDir()
	configPath := writeConfigarrYAML(t, dir, "config.yml", `
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: sonarr-key-1
`)
	req := oas.NewOptConfigarrImportRequest(oas.ConfigarrImportRequest{
		ConfigPath: oas.NewOptString(configPath),
	})
	res, err := f.h.ImportConfigarr(adminCtx, req)
	if err != nil || res.Imported != 1 {
		t.Fatalf("ImportConfigarr() = %+v, %v; want 1 imported", res, err)
	}
	instList, instErr := f.h.instances.List(adminCtx)
	if instErr != nil || len(instList) != 1 || instList[0].Name != "series" {
		t.Fatalf("instances after import: len=%d, %v", len(instList), instErr)
	}
	st, stErr := f.h.GetConfigarrStatus(adminCtx)
	if stErr != nil || st.Linked || !st.LastResult.Set || st.LastResult.Value.Imported != 1 || !st.LastImportAt.Set {
		t.Fatalf("GetConfigarrStatus() after import = %+v, %v", st, stErr)
	}
}

func testConfigarrImportRefused(t *testing.T, f *integrationFixture, ctx context.Context, configPath string, wantErr error) {
	t.Helper()
	req := oas.NewOptConfigarrImportRequest(oas.ConfigarrImportRequest{
		ConfigPath: oas.NewOptString(configPath),
	})
	_, err := f.h.ImportConfigarr(ctx, req)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ImportConfigarr() err = %v, want %v", err, wantErr)
	}
	instList, listErr := f.h.instances.List(ctx)
	if listErr != nil || len(instList) != 0 {
		t.Fatalf("instances after refused import: len=%d, %v", len(instList), listErr)
	}
}

func TestConfigarrOperations(t *testing.T) {
	t.Parallel()
	t.Run("positive import and status", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		adminCtx := withUser(t.Context(), f.admin)
		testConfigarrImportPositiveAndStatus(t, f, adminCtx)
	})
	t.Run("negative import no user", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		dir := t.TempDir()
		p := writeConfigarrYAML(t, dir, "config.yml", "sonarr:\n  s:\n    base_url: http://s:8989\n    api_key: k\n")
		testConfigarrImportRefused(t, f, t.Context(), p, domain.ErrUnauthorized)
	})
	t.Run("negative import viewer", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		dir := t.TempDir()
		p := writeConfigarrYAML(t, dir, "config.yml", "sonarr:\n  s:\n    base_url: http://s:8989\n    api_key: k\n")
		testConfigarrImportRefused(t, f, withUser(t.Context(), f.viewer), p, domain.ErrForbidden)
	})
	t.Run("negative import invalid no path", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		adminCtx := withUser(t.Context(), f.admin)
		_, err := f.h.ImportConfigarr(adminCtx, oas.OptConfigarrImportRequest{})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ImportConfigarr() empty path err = %v, want ErrInvalid", err)
		}
	})
	t.Run("negative import invalid yaml", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		dir := t.TempDir()
		badYAML := writeConfigarrYAML(t, dir, "bad.yml", "not: [valid: yaml: 123")
		adminCtx := withUser(t.Context(), f.admin)
		testConfigarrImportRefused(t, f, adminCtx, badYAML, domain.ErrInvalid)
	})
	t.Run("negative get configarr status no user", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.GetConfigarrStatus(t.Context())
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("GetConfigarrStatus() err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("negative get configarr status viewer", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.GetConfigarrStatus(withUser(t.Context(), f.viewer))
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("GetConfigarrStatus() err = %v, want ErrForbidden", err)
		}
	})
}
