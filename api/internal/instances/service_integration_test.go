// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package instances_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

const (
	booksURL  = "http://books.invalid:8787"
	otherURL  = "http://other.invalid:8787"
	testKey   = "a-key-for-the-stub"
	configKey = "readarr.books"
	// productCheck is the name Postgres gives the check of the column last_seen_product.
	productCheck = "instances_last_seen_product_check"
)

// errRefused is what the stub answers for an instance that does not respond.
var errRefused = errors.New("connection refused by the test")

// The answers a connection test gets, as recorded from the images on 2026-10-11.
var (
	bookshelf = arrclient.Result{AppName: "Readarr", Version: "0.4.21.182", Product: domain.ProductBookshelf}
	chaptarr  = arrclient.Result{AppName: "Chaptarr", Version: "0.9.965.0", Product: domain.ProductChaptarr}
	sonarr    = arrclient.Result{AppName: "Sonarr", Version: "4.0.10.2544"}
)

// stubProber answers every probe with what the current step set.
type stubProber struct {
	res arrclient.Result
	err error
}

func (p *stubProber) Probe(context.Context, domain.AppKind, string, string) (arrclient.Result, error) {
	return p.res, p.err
}

type fixture struct {
	svc   *instances.Service
	st    *store.Store
	clk   *clockwork.FakeClock
	probe *stubProber
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st := storetest.New(t)
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	clk := clock.NewFake()
	probe := &stubProber{}
	svc := instances.New(st, enc, clk, id.New(), probe, slog.New(slog.DiscardHandler))
	return fixture{svc: svc, st: st, clk: clk, probe: probe}
}

func readarrInput(baseURL string) instances.Input {
	return instances.Input{Kind: domain.KindReadarr, Name: "books", BaseURL: baseURL, APIKey: testKey, Enabled: true}
}

// step is one thing that happens to a stored instance.
type step func(t *testing.T, f fixture, instID uuid.UUID)

// answers is a connection test the instance answers.
func answers(res arrclient.Result) step {
	return func(t *testing.T, f fixture, instID uuid.UUID) {
		t.Helper()
		f.probe.res, f.probe.err = res, nil
		if _, err := f.svc.Test(t.Context(), instID); err != nil {
			t.Fatalf("Test() = %v", err)
		}
	}
}

// fails is a connection test the instance does not answer.
func fails(t *testing.T, f fixture, instID uuid.UUID) {
	t.Helper()
	f.probe.res, f.probe.err = arrclient.Result{}, errRefused
	if _, err := f.svc.Test(t.Context(), instID); !errors.Is(err, errRefused) {
		t.Fatalf("Test() = %v, want %v", err, errRefused)
	}
}

// edited is an update of the instance by hand. An empty API key keeps the stored one.
func edited(in instances.Input) step {
	return func(t *testing.T, f fixture, instID uuid.UUID) {
		t.Helper()
		in.APIKey = ""
		if _, err := f.svc.Update(t.Context(), instID, in); err != nil {
			t.Fatalf("Update() = %v", err)
		}
	}
}

type productCase struct {
	name        string
	kind        domain.AppKind
	steps       []step
	want        domain.Product
	wantVersion string
	wantFailed  bool
}

func (tc productCase) run(t *testing.T) {
	t.Helper()
	f := newFixture(t)
	in := readarrInput(booksURL)
	if tc.kind != "" {
		in.Kind = tc.kind
	}
	created, err := f.svc.Create(t.Context(), in)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	for _, s := range tc.steps {
		s(t, f, created.ID)
	}
	got, err := f.svc.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.LastSeenProduct != tc.want {
		t.Errorf("product = %q, want %q", got.LastSeenProduct, tc.want)
	}
	if got.LastSeenVersion != tc.wantVersion {
		t.Errorf("version = %q, want %q", got.LastSeenVersion, tc.wantVersion)
	}
	if failed := got.LastError != ""; failed != tc.wantFailed {
		t.Errorf("last error = %q, want one = %v", got.LastError, tc.wantFailed)
	}
}

func TestService_Test_StoresTheProduct(t *testing.T) {
	t.Parallel()
	renamed := readarrInput(booksURL)
	renamed.Name = "books, renamed"
	lidarr := readarrInput(booksURL)
	lidarr.Kind = domain.KindLidarr
	cases := []productCase{
		// Positive.
		{name: "a test stores the fork it reached", steps: []step{answers(bookshelf)}, want: domain.ProductBookshelf, wantVersion: bookshelf.Version},
		{name: "a later test replaces the product", steps: []step{answers(bookshelf), answers(chaptarr)}, want: domain.ProductChaptarr, wantVersion: chaptarr.Version},
		{
			name:  "an edit that keeps kind and address keeps the product",
			steps: []step{answers(bookshelf), edited(renamed)}, want: domain.ProductBookshelf, wantVersion: bookshelf.Version,
		},
		// Negative.
		{name: "a failed test keeps the product", steps: []step{answers(bookshelf), fails}, want: domain.ProductBookshelf, wantFailed: true},
		{name: "a failed first test stores none", steps: []step{fails}, want: domain.ProductNone, wantFailed: true},
		{
			name:  "a new address forgets the product",
			steps: []step{answers(bookshelf), edited(readarrInput(otherURL))}, want: domain.ProductNone, wantVersion: bookshelf.Version,
		},
		{
			name:  "a new kind forgets the product",
			steps: []step{answers(bookshelf), edited(lidarr)}, want: domain.ProductNone, wantVersion: bookshelf.Version,
		},
		// Boundary.
		{name: "no test yet", want: domain.ProductNone},
		{name: "a kind one product serves stores none", kind: domain.KindSonarr, steps: []step{answers(sonarr)}, want: domain.ProductNone, wantVersion: sonarr.Version},
		{
			name:  "an answer without a product clears an earlier one",
			steps: []step{answers(bookshelf), answers(sonarr)}, want: domain.ProductNone, wantVersion: sonarr.Version,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// Boundary: the column accepts exactly the products of the API enumeration, so a product
// added to the OpenAPI document fails here until a migration widens the column's check.
func TestService_Test_StoresEveryProductOfTheAPI(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), readarrInput(booksURL))
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	all := oas.ArrProduct("").AllValues()
	if len(all) == 0 {
		t.Fatal("the API enumerates no product")
	}
	for _, value := range all {
		product := domain.Product(value)
		answers(arrclient.Result{AppName: "Readarr", Version: "0.4.18.2805", Product: product})(t, f, created.ID)
		got, getErr := f.svc.Get(t.Context(), created.ID)
		if getErr != nil {
			t.Fatalf("Get() = %v", getErr)
		}
		if got.LastSeenProduct != product {
			t.Errorf("product = %q, want %q", got.LastSeenProduct, product)
		}
	}
	// Negative: the store refuses a value outside the enumeration.
	err = f.st.Q().RecordInstanceCheck(t.Context(), sqlcgen.RecordInstanceCheckParams{
		ID: created.ID, LastSeenProduct: new("a_later_product"), LastCheckAt: new(f.clk.Now()),
	})
	if err == nil || !strings.Contains(err.Error(), productCheck) {
		t.Errorf("RecordInstanceCheck() with a product outside the enumeration = %v, want the check %s to refuse it", err, productCheck)
	}
}

func TestService_UpsertConfigarr_Product(t *testing.T) {
	t.Parallel()
	lidarr := readarrInput(booksURL)
	lidarr.Kind = domain.KindLidarr
	cases := []struct {
		name  string
		again instances.Input
		want  domain.Product
	}{
		// Positive: the re-import of an unchanged entry keeps what the test found.
		{name: "the same kind and address keep the product", again: readarrInput(booksURL), want: domain.ProductBookshelf},
		// Negative.
		{name: "a new address forgets the product", again: readarrInput(otherURL), want: domain.ProductNone},
		{name: "a new kind forgets the product", again: lidarr, want: domain.ProductNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			first, err := f.svc.UpsertConfigarr(t.Context(), configKey, readarrInput(booksURL))
			if err != nil || !first.Inserted {
				t.Fatalf("first UpsertConfigarr() = %+v, %v, want an insert", first, err)
			}
			answers(bookshelf)(t, f, first.Instance.ID)
			second, err := f.svc.UpsertConfigarr(t.Context(), configKey, tc.again)
			if err != nil || second.Inserted {
				t.Fatalf("second UpsertConfigarr() = %+v, %v, want an update", second, err)
			}
			if second.Instance.LastSeenProduct != tc.want {
				t.Errorf("product after the re-import = %q, want %q", second.Instance.LastSeenProduct, tc.want)
			}
			got, err := f.svc.Get(t.Context(), first.Instance.ID)
			if err != nil {
				t.Fatalf("Get() = %v", err)
			}
			if got.LastSeenProduct != tc.want {
				t.Errorf("stored product = %q, want %q", got.LastSeenProduct, tc.want)
			}
		})
	}
}

type deleteTestCase struct {
	name    string
	setup   func(t *testing.T, f fixture) uuid.UUID
	wantErr error
	check   func(t *testing.T, f fixture, id uuid.UUID)
}

func (tc deleteTestCase) run(t *testing.T) {
	t.Helper()
	f := newFixture(t)
	instID := tc.setup(t, f)
	err := f.svc.Delete(t.Context(), instID)
	if tc.wantErr == nil {
		if err != nil {
			t.Fatalf("Delete() = %v, want nil", err)
		}
	} else {
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("Delete() = %v, want %v", err, tc.wantErr)
		}
	}
	if tc.check != nil {
		tc.check(t, f, instID)
	}
}

func casesPositive() []deleteTestCase {
	return []deleteTestCase{
		{
			name: "positive: manual row is deleted",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				created, err := f.svc.Create(t.Context(), readarrInput(booksURL))
				if err != nil {
					t.Fatalf("Create() = %v", err)
				}
				return created.ID
			},
			wantErr: nil,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("Get() = %v, want ErrNotFound", err)
				}
			},
		},
		{
			name: "positive: configarr row orphaned can be deleted",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				out, err := f.svc.UpsertConfigarr(t.Context(), configKey, readarrInput(booksURL))
				if err != nil {
					t.Fatalf("UpsertConfigarr() = %v", err)
				}
				n, err := f.svc.DisableConfigarrNotIn(t.Context(), []string{"other.key"})
				if err != nil || n != 1 {
					t.Fatalf("DisableConfigarrNotIn() = %d, %v, want 1", n, err)
				}
				return out.Instance.ID
			},
			wantErr: nil,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("Get() = %v, want ErrNotFound", err)
				}
			},
		},
	}
}

func casesNegative() []deleteTestCase {
	return []deleteTestCase{
		{
			name: "negative: configarr row declared enabled refuses delete",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				out, err := f.svc.UpsertConfigarr(t.Context(), configKey, readarrInput(booksURL))
				if err != nil {
					t.Fatalf("UpsertConfigarr() = %v", err)
				}
				return out.Instance.ID
			},
			wantErr: domain.ErrForbidden,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); err != nil {
					t.Fatalf("Get() = %v, want row still there", err)
				}
			},
		},
		{
			name: "negative: configarr row declared enabled:false refuses delete",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				in := readarrInput(booksURL)
				in.Enabled = false
				out, err := f.svc.UpsertConfigarr(t.Context(), configKey, in)
				if err != nil {
					t.Fatalf("UpsertConfigarr() = %v", err)
				}
				return out.Instance.ID
			},
			wantErr: domain.ErrForbidden,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); err != nil {
					t.Fatalf("Get() = %v, want row still there", err)
				}
			},
		},
		{
			name: "negative: unknown id returns not found",
			setup: func(t *testing.T, _ fixture) uuid.UUID {
				t.Helper()
				return uuid.New()
			},
			wantErr: domain.ErrNotFound,
			check:   nil,
		},
	}
}

func casesBoundary() []deleteTestCase {
	return []deleteTestCase{
		{
			name: "boundary: orphaned then declared again refuses delete",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				out, err := f.svc.UpsertConfigarr(t.Context(), configKey, readarrInput(booksURL))
				if err != nil {
					t.Fatalf("UpsertConfigarr() = %v", err)
				}
				if _, err := f.svc.DisableConfigarrNotIn(t.Context(), nil); err != nil {
					t.Fatalf("DisableConfigarrNotIn() = %v", err)
				}
				if _, err := f.svc.UpsertConfigarr(t.Context(), configKey, readarrInput(booksURL)); err != nil {
					t.Fatalf("second UpsertConfigarr() = %v", err)
				}
				return out.Instance.ID
			},
			wantErr: domain.ErrForbidden,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); err != nil {
					t.Fatalf("Get() = %v, want row still there", err)
				}
			},
		},
		{
			name: "boundary: declared enabled:false then orphaned can be deleted",
			setup: func(t *testing.T, f fixture) uuid.UUID {
				t.Helper()
				in := readarrInput(booksURL)
				in.Enabled = false
				out, err := f.svc.UpsertConfigarr(t.Context(), configKey, in)
				if err != nil {
					t.Fatalf("UpsertConfigarr() = %v", err)
				}
				n, err := f.svc.DisableConfigarrNotIn(t.Context(), nil)
				if err != nil || n != 1 {
					t.Fatalf("DisableConfigarrNotIn() = %d, %v, want 1", n, err)
				}
				return out.Instance.ID
			},
			wantErr: nil,
			check: func(t *testing.T, f fixture, id uuid.UUID) {
				t.Helper()
				if _, err := f.svc.Get(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("Get() = %v, want ErrNotFound", err)
				}
			},
		},
	}
}

func TestService_Delete_ConfigarrRule(t *testing.T) {
	t.Parallel()
	cases := slices.Concat(casesPositive(), casesNegative(), casesBoundary())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
