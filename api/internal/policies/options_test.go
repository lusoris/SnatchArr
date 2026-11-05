// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policies

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

type fakeInstanceReader struct {
	inst     domain.Instance
	instErr  error
	creds    instances.Credentials
	credsErr error
	calls    atomic.Int64
}

func (f *fakeInstanceReader) GetWithCredentials(context.Context, uuid.UUID) (domain.Instance, instances.Credentials, error) {
	f.calls.Add(1)
	if f.instErr != nil {
		return domain.Instance{}, instances.Credentials{}, f.instErr
	}
	if f.credsErr != nil {
		return domain.Instance{}, instances.Credentials{}, f.credsErr
	}
	return f.inst, f.creds, nil
}

type fakeProfileLanguager struct {
	profiles []arrclient.ProfileLanguage
	err      error
	calls    atomic.Int64
}

func (f *fakeProfileLanguager) ProfileLanguages(context.Context, domain.AppKind, string, string) ([]arrclient.ProfileLanguage, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.profiles, nil
}

type dummyLabeler struct{}

func (dummyLabeler) Labels(context.Context, domain.AppKind, string, string) (arrclient.Labels, error) {
	return arrclient.Labels{}, nil
}

func TestForeignPreflight(t *testing.T) {
	t.Parallel()

	instID := uuid.New()

	t.Run("positive radarr with any, original, german, english, missing profiles", func(t *testing.T) {
		t.Parallel()
		reader := &fakeInstanceReader{
			inst:  domain.Instance{ID: instID, Kind: domain.KindRadarr, Name: "radarr-main"},
			creds: instances.Credentials{BaseURL: "http://radarr:7878", APIKey: "test-key"},
		}
		pl := &fakeProfileLanguager{
			profiles: []arrclient.ProfileLanguage{
				{ID: 1, Name: "Any", Language: &foreigntitle.RadarrLanguage{ID: -1, Name: "Any"}},
				{ID: 2, Name: "Original", Language: &foreigntitle.RadarrLanguage{ID: -2, Name: "Original"}},
				{ID: 3, Name: "German", Language: &foreigntitle.RadarrLanguage{ID: 4, Name: "German"}},
				{ID: 4, Name: "English", Language: &foreigntitle.RadarrLanguage{ID: 1, Name: "English"}},
				{ID: 5, Name: "NoLang", Language: nil},
			},
		}

		opts := newOptionsWithReader(reader, dummyLabeler{}, pl)
		got, err := opts.ForeignPreflight(t.Context(), instID)
		if err != nil {
			t.Fatalf("ForeignPreflight() error = %v", err)
		}

		if !got.Applies {
			t.Errorf("got.Applies = false, want true")
		}
		wantLangs := []string{"de", "fr"}
		if !reflect.DeepEqual(got.Languages, wantLangs) {
			t.Errorf("got.Languages = %v, want %v", got.Languages, wantLangs)
		}
		if len(got.Profiles) != 5 {
			t.Fatalf("len(got.Profiles) = %d, want 5", len(got.Profiles))
		}

		// Profile 1: Any
		p1 := got.Profiles[0]
		if p1.ID != 1 || p1.Name != "Any" {
			t.Errorf("p1 = %+v", p1)
		}
		wantP1Verdicts := []LanguageVerdict{
			{Language: "de", Verdict: foreigntitle.VerdictAllowed, Reason: foreigntitle.ReasonProfileAny},
			{Language: "fr", Verdict: foreigntitle.VerdictAllowed, Reason: foreigntitle.ReasonProfileAny},
		}
		if !reflect.DeepEqual(p1.Verdicts, wantP1Verdicts) {
			t.Errorf("p1.Verdicts = %+v, want %+v", p1.Verdicts, wantP1Verdicts)
		}

		// Profile 2: Original
		p2 := got.Profiles[1]
		wantP2Verdicts := []LanguageVerdict{
			{Language: "de", Verdict: foreigntitle.VerdictOriginalOnly, Reason: foreigntitle.ReasonProfileOriginal},
			{Language: "fr", Verdict: foreigntitle.VerdictOriginalOnly, Reason: foreigntitle.ReasonProfileOriginal},
		}
		if !reflect.DeepEqual(p2.Verdicts, wantP2Verdicts) {
			t.Errorf("p2.Verdicts = %+v, want %+v", p2.Verdicts, wantP2Verdicts)
		}

		// Profile 3: German
		p3 := got.Profiles[2]
		wantP3Verdicts := []LanguageVerdict{
			{Language: "de", Verdict: foreigntitle.VerdictAllowed, Reason: foreigntitle.ReasonProfileLanguageMatch},
			{Language: "fr", Verdict: foreigntitle.VerdictSkipped, Reason: foreigntitle.ReasonProfileLanguageMismatch},
		}
		if !reflect.DeepEqual(p3.Verdicts, wantP3Verdicts) {
			t.Errorf("p3.Verdicts = %+v, want %+v", p3.Verdicts, wantP3Verdicts)
		}

		// Profile 4: English
		p4 := got.Profiles[3]
		wantP4Verdicts := []LanguageVerdict{
			{Language: "de", Verdict: foreigntitle.VerdictSkipped, Reason: foreigntitle.ReasonProfileLanguageMismatch},
			{Language: "fr", Verdict: foreigntitle.VerdictSkipped, Reason: foreigntitle.ReasonProfileLanguageMismatch},
		}
		if !reflect.DeepEqual(p4.Verdicts, wantP4Verdicts) {
			t.Errorf("p4.Verdicts = %+v, want %+v", p4.Verdicts, wantP4Verdicts)
		}

		// Profile 5: NoLang
		p5 := got.Profiles[4]
		wantP5Verdicts := []LanguageVerdict{
			{Language: "de", Verdict: foreigntitle.VerdictSkipped, Reason: foreigntitle.ReasonProfileLanguageMissing},
			{Language: "fr", Verdict: foreigntitle.VerdictSkipped, Reason: foreigntitle.ReasonProfileLanguageMissing},
		}
		if !reflect.DeepEqual(p5.Verdicts, wantP5Verdicts) {
			t.Errorf("p5.Verdicts = %+v, want %+v", p5.Verdicts, wantP5Verdicts)
		}
	})

	t.Run("negative non-radarr returns applies false and no calls", func(t *testing.T) {
		t.Parallel()
		reader := &fakeInstanceReader{
			inst: domain.Instance{ID: instID, Kind: domain.KindSonarr, Name: "sonarr-main"},
		}
		pl := &fakeProfileLanguager{}
		opts := newOptionsWithReader(reader, dummyLabeler{}, pl)

		got, err := opts.ForeignPreflight(t.Context(), instID)
		if err != nil {
			t.Fatalf("ForeignPreflight() error = %v", err)
		}
		if got.Applies {
			t.Errorf("got.Applies = true, want false")
		}
		if got.Languages == nil || len(got.Languages) != 0 {
			t.Errorf("got.Languages = %v, want non-nil empty", got.Languages)
		}
		if got.Profiles == nil || len(got.Profiles) != 0 {
			t.Errorf("got.Profiles = %v, want non-nil empty", got.Profiles)
		}
		if pl.calls.Load() != 0 {
			t.Errorf("pl.calls = %d, want 0", pl.calls.Load())
		}
	})

	t.Run("negative upstream error wrapped", func(t *testing.T) {
		t.Parallel()
		upstreamErr := errors.New("connection reset by peer")
		reader := &fakeInstanceReader{
			inst:  domain.Instance{ID: instID, Kind: domain.KindRadarr, Name: "radarr-main"},
			creds: instances.Credentials{BaseURL: "http://radarr:7878", APIKey: "test-key"},
		}
		pl := &fakeProfileLanguager{err: upstreamErr}
		opts := newOptionsWithReader(reader, dummyLabeler{}, pl)

		_, err := opts.ForeignPreflight(t.Context(), instID)
		if err == nil || !errors.Is(err, upstreamErr) {
			t.Fatalf("ForeignPreflight() error = %v, want wrapping %v", err, upstreamErr)
		}
	})
}

func TestOptionsSnapshot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		instErr, credsErr error
	}{
		{name: "positive empty upstream lists"},
		{name: "negative missing instance", instErr: domain.ErrNotFound},
		{name: "negative decrypt failure", credsErr: errors.New("invalid ciphertext")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, preflight := range []bool{true, false} {
				reader := &fakeInstanceReader{inst: domain.Instance{Kind: domain.KindRadarr}, instErr: tc.instErr, credsErr: tc.credsErr}
				profiles := &fakeProfileLanguager{}
				opts := newOptionsWithReader(reader, dummyLabeler{}, profiles)
				var err error
				if preflight {
					_, err = opts.ForeignPreflight(t.Context(), uuid.Nil)
				} else {
					_, err = opts.FilterOptions(t.Context(), uuid.Nil)
				}
				want := tc.instErr
				if want == nil {
					want = tc.credsErr
				}
				if !errors.Is(err, want) {
					t.Fatalf("preflight %t: error %v; want %v", preflight, err, want)
				}
				if reader.calls.Load() != 1 {
					t.Fatalf("snapshot reads = %d; want 1", reader.calls.Load())
				}
				if want != nil && profiles.calls.Load() != 0 {
					t.Fatal("upstream called after snapshot failure")
				}
			}
		})
	}
}
