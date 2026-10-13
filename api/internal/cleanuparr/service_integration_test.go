// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package cleanuparr_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/cleanuparr"
	"github.com/lusoris/SnatchArr/api/internal/cleanuparrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

const (
	firstKey  = "first-key-0001"
	secondKey = "second-key-0002"
	linkURL   = "http://cleanuparr.invalid:11011"
)

var errRefused = errors.New("connection refused by the test")

// stubClient answers like Cleanuparr and records the key of every call.
type stubClient struct {
	mu    sync.Mutex
	keys  []string
	calls int
	err   error
}

func (c *stubClient) seen(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	return c.err
}

func (c *stubClient) Status(_ context.Context, _, key string) (cleanuparrclient.Status, error) {
	return cleanuparrclient.Status{Version: "2.4.0"}, c.seen(key)
}

func (c *stubClient) Summary(_ context.Context, _, key string) (cleanuparrclient.StrikeSummary, error) {
	return cleanuparrclient.StrikeSummary{StruckDownloads: 3}, c.seen(key)
}

func (c *stubClient) RecentStrikes(_ context.Context, _, key string, _ int) ([]cleanuparrclient.Strike, error) {
	return []cleanuparrclient.Strike{{ID: "s1", Type: "Stalled"}}, c.seen(key)
}

func (c *stubClient) lastKey() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.keys) == 0 {
		return "", c.calls
	}
	return c.keys[len(c.keys)-1], c.calls
}

type fixture struct {
	svc    *cleanuparr.Service
	st     *store.Store
	clk    *clockwork.FakeClock
	client *stubClient
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	st := storetest.New(t)
	clk := clock.NewFake()
	client := &stubClient{}
	svc := cleanuparr.New(st, enc, clk, id.New(), client, slog.New(slog.DiscardHandler))
	return fixture{svc: svc, st: st, clk: clk, client: client}
}

func (f fixture) create(t *testing.T) domain.CleanuparrLink {
	t.Helper()
	link, err := f.svc.Create(t.Context(), cleanuparr.Input{Name: " cleanup ", BaseURL: linkURL + "/", APIKey: " " + firstKey + " ", Enabled: true})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return link
}

func TestCreateNormalisesAndSealsTheKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if link.Name != "cleanup" || link.BaseURL != linkURL || !link.Enabled {
		t.Errorf("Create() = %+v, want the trimmed name and URL", link)
	}
	row, err := f.st.Q().GetCleanuparrLink(t.Context(), link.ID)
	if err != nil {
		t.Fatalf("GetCleanuparrLink() = %v", err)
	}
	if bytes.Contains(row.ApiKeyEnc, []byte(firstKey)) {
		t.Error("the stored api key holds the plaintext key")
	}
	if _, err = f.svc.Test(t.Context(), link.ID); err != nil {
		t.Fatalf("Test() = %v", err)
	}
	if key, _ := f.client.lastKey(); key != firstKey {
		t.Errorf("Cleanuparr was called with key %q, want the trimmed key %q", key, firstKey)
	}
	listed, err := f.svc.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].ID != link.ID {
		t.Errorf("List() = %+v, %v; want the one link", listed, err)
	}
}

func TestUpdateKeepsOrReplacesTheKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, key, want string
	}{
		{"positive empty key keeps the stored one", "", firstKey},
		{"positive new key replaces it", secondKey, secondKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			link := f.create(t)
			updated, err := f.svc.Update(t.Context(), link.ID, cleanuparr.Input{Name: "renamed", BaseURL: linkURL, APIKey: tc.key, Enabled: true})
			if err != nil || updated.Name != "renamed" {
				t.Fatalf("Update() = %+v, %v; want the new name", updated, err)
			}
			if _, err := f.svc.Test(t.Context(), link.ID); err != nil {
				t.Fatalf("Test() = %v", err)
			}
			if key, _ := f.client.lastKey(); key != tc.want {
				t.Errorf("Cleanuparr was called with key %q, want %q", key, tc.want)
			}
		})
	}
}

func TestLinkRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	valid := cleanuparr.Input{Name: "cleanup", BaseURL: linkURL, APIKey: firstKey}
	shortKey := valid
	shortKey.APIKey = "1234567"
	longName := valid
	longName.Name = strings.Repeat("n", 65)
	badScheme := valid
	badScheme.BaseURL = "ftp://cleanuparr.invalid"
	cases := []struct {
		name string
		call func(context.Context) error
		want error
	}{
		{"boundary key one short of eight characters", func(ctx context.Context) error { _, err := f.svc.Create(ctx, shortKey); return err }, domain.ErrInvalid},
		{"boundary name one over 64 characters", func(ctx context.Context) error { _, err := f.svc.Create(ctx, longName); return err }, domain.ErrInvalid},
		{"negative scheme other than http", func(ctx context.Context) error { _, err := f.svc.Update(ctx, link.ID, badScheme); return err }, domain.ErrInvalid},
		{"negative test input with a short key", func(ctx context.Context) error { _, err := f.svc.TestInput(ctx, shortKey); return err }, domain.ErrInvalid},
		{"negative get of an unknown link", func(ctx context.Context) error { _, err := f.svc.Get(ctx, uuid.New()); return err }, domain.ErrNotFound},
		{"negative update of an unknown link", func(ctx context.Context) error { _, err := f.svc.Update(ctx, uuid.New(), valid); return err }, domain.ErrNotFound},
		{"negative test of an unknown link", func(ctx context.Context) error { _, err := f.svc.Test(ctx, uuid.New()); return err }, domain.ErrNotFound},
		{"negative delete of an unknown link", func(ctx context.Context) error { return f.svc.Delete(ctx, uuid.New()) }, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(t.Context()); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if _, calls := f.client.lastKey(); calls != 0 {
				t.Errorf("refused calls reached Cleanuparr %d times, want 0", calls)
			}
		})
	}
}

func TestDeleteRemovesTheLink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if err := f.svc.Delete(t.Context(), link.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, err := f.svc.Get(t.Context(), link.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() after Delete() = %v, want %v", err, domain.ErrNotFound)
	}
}

// TestTamperedKeyIsReportedNotSent covers the encryption at rest: a stored key that does not
// open is never sent to Cleanuparr and leaves the link unreachable with the cause.
func TestTamperedKeyIsReportedNotSent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	row, err := f.st.Q().GetCleanuparrLink(t.Context(), link.ID)
	if err != nil {
		t.Fatalf("GetCleanuparrLink() = %v", err)
	}
	tampered := bytes.Clone(row.ApiKeyEnc)
	tampered[len(tampered)-1] ^= 0x01
	if _, err = f.st.Q().UpdateCleanuparrLink(t.Context(), sqlcgen.UpdateCleanuparrLinkParams{
		ID: row.ID, Name: row.Name, BaseUrl: row.BaseUrl, ApiKeyEnc: tampered, Enabled: row.Enabled, UpdatedAt: f.clk.Now(),
	}); err != nil {
		t.Fatalf("UpdateCleanuparrLink() = %v", err)
	}
	if _, err = f.svc.Test(t.Context(), link.ID); !errors.Is(err, domain.ErrUnreachable) {
		t.Errorf("Test() = %v, want %v", err, domain.ErrUnreachable)
	}
	if _, calls := f.client.lastKey(); calls != 0 {
		t.Errorf("Cleanuparr was called %d times with an undecryptable key, want 0", calls)
	}
	got, err := f.svc.Get(t.Context(), link.ID)
	if err != nil || got.LastError != "stored api key cannot be decrypted" {
		t.Errorf("Get() = %+v, %v; want the decryption failure recorded", got, err)
	}
}

func TestStatusReusesTheViewForItsTTL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	first, err := f.svc.Status(t.Context())
	if err != nil || len(first) != 1 || !first[0].Reachable || first[0].Summary.StruckDownloads != 3 || len(first[0].Recent) != 1 {
		t.Fatalf("Status() = %+v, %v; want one reachable link with its strikes", first, err)
	}
	_, calls := f.client.lastKey()
	f.clk.Advance(cleanuparr.StatusTTL - time.Nanosecond)
	if _, err = f.svc.Status(t.Context()); err != nil {
		t.Fatalf("Status() inside the TTL = %v", err)
	}
	if _, again := f.client.lastKey(); again != calls {
		t.Errorf("Status() inside the TTL called Cleanuparr %d more times, want 0", again-calls)
	}
	f.client.err = errRefused
	f.clk.Advance(time.Nanosecond)
	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 1 || views[0].Reachable || !strings.Contains(views[0].Error, errRefused.Error()) {
		t.Fatalf("Status() at the TTL = %+v, %v; want the link unreachable with the cause", views, err)
	}
	if got, err := f.svc.Get(t.Context(), link.ID); err != nil || !strings.Contains(got.LastError, errRefused.Error()) {
		t.Errorf("Get() = %+v, %v; want the failed check recorded", got, err)
	}
}

func TestUpdateDropsTheCachedView(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if _, err := f.svc.Status(t.Context()); err != nil {
		t.Fatalf("Status() = %v", err)
	}
	if _, err := f.svc.Update(t.Context(), link.ID, cleanuparr.Input{Name: "renamed", BaseURL: linkURL, APIKey: secondKey, Enabled: true}); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 1 || views[0].Link.Name != "renamed" {
		t.Fatalf("Status() right after Update() = %+v, %v; want a fresh view of the renamed link", views, err)
	}
	if key, _ := f.client.lastKey(); key != secondKey {
		t.Errorf("Status() right after Update() checked with %q, want the new key %q", key, secondKey)
	}
}

func TestStatusSkipsDisabledLinks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if _, err := f.svc.Update(t.Context(), link.ID, cleanuparr.Input{Name: "cleanup", BaseURL: linkURL, Enabled: false}); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 0 {
		t.Errorf("Status() = %+v, %v; want no view of a disabled link", views, err)
	}
}

func TestTestInputSendsTheTrimmedKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	st, err := f.svc.TestInput(t.Context(), cleanuparr.Input{Name: "probe", BaseURL: linkURL, APIKey: " " + secondKey})
	if err != nil || st.Version != "2.4.0" {
		t.Fatalf("TestInput() = %+v, %v", st, err)
	}
	if key, _ := f.client.lastKey(); key != secondKey {
		t.Errorf("Cleanuparr was called with key %q, want %q", key, secondKey)
	}
	f.client.err = errRefused
	if _, err := f.svc.TestInput(t.Context(), cleanuparr.Input{Name: "probe", BaseURL: linkURL, APIKey: secondKey}); !errors.Is(err, errRefused) {
		t.Errorf("TestInput() against a refusing Cleanuparr = %v, want %v", err, errRefused)
	}
}
