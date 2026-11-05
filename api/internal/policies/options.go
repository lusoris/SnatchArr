// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policies

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

// LanguageVerdict pairs a foreign title language with its preflight compatibility verdict and reason.
type LanguageVerdict struct {
	Language string
	Verdict  foreigntitle.Verdict
	Reason   foreigntitle.Reason
}

// ProfilePreflight represents a Radarr quality profile and its language compatibility verdicts.
type ProfilePreflight struct {
	ID       int32
	Name     string
	Language *foreigntitle.RadarrLanguage
	Verdicts []LanguageVerdict
}

// Preflight summarizes the foreign-search language preflight compatibility for an instance.
type Preflight struct {
	Applies   bool
	Languages []string
	Profiles  []ProfilePreflight
}

type instanceReader interface {
	GetWithCredentials(ctx context.Context, instID uuid.UUID) (domain.Instance, instances.Credentials, error)
}

// Options reads what an instance offers for the policy filters and foreign-search preflight.
type Options struct {
	instances    instanceReader
	labels       arrclient.Labeler
	profileLangs arrclient.ProfileLanguager

	langsOnce sync.Once
	langs     []string
	langsErr  error
}

// NewOptions wires the filter options reader and foreign preflight checker.
func NewOptions(inst *instances.Service, labels arrclient.Labeler, profileLangs arrclient.ProfileLanguager) *Options {
	return newOptionsWithReader(inst, labels, profileLangs)
}

func newOptionsWithReader(inst instanceReader, labels arrclient.Labeler, profileLangs arrclient.ProfileLanguager) *Options {
	return &Options{
		instances:    inst,
		labels:       labels,
		profileLangs: profileLangs,
	}
}

// FilterOptions returns the tags and quality profiles of a stored instance.
func (o *Options) FilterOptions(ctx context.Context, instanceID uuid.UUID) (arrclient.Labels, error) {
	inst, creds, err := o.instances.GetWithCredentials(ctx, instanceID)
	if err != nil {
		return arrclient.Labels{}, err
	}
	labels, err := o.labels.Labels(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	if err != nil {
		return arrclient.Labels{}, fmt.Errorf("policies: filter options: %w", err)
	}
	return labels, nil
}

// ForeignPreflight evaluates whether Radarr's quality profiles allow foreign-title searches in supported languages.
func (o *Options) ForeignPreflight(ctx context.Context, instanceID uuid.UUID) (Preflight, error) {
	inst, creds, err := o.instances.GetWithCredentials(ctx, instanceID)
	if err != nil {
		return Preflight{}, err
	}
	if inst.Kind != domain.KindRadarr {
		return Preflight{
			Applies:   false,
			Languages: []string{},
			Profiles:  []ProfilePreflight{},
		}, nil
	}
	langs, err := o.supportedLanguages()
	if err != nil {
		return Preflight{}, err
	}
	profiles, err := o.profileLangs.ProfileLanguages(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	if err != nil {
		return Preflight{}, fmt.Errorf("policies: foreign preflight: %w", err)
	}
	return Preflight{
		Applies:   true,
		Languages: slices.Clone(langs),
		Profiles:  buildProfilePreflights(profiles, langs),
	}, nil
}

func (o *Options) supportedLanguages() ([]string, error) {
	o.langsOnce.Do(func() {
		rules, err := foreigntitle.Load()
		if err != nil {
			o.langsErr = fmt.Errorf("policies: load foreign title rules: %w", err)
			return
		}
		keys := make([]string, 0, len(rules))
		for k := range rules {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		o.langs = keys
	})
	if o.langsErr != nil {
		return nil, o.langsErr
	}
	return o.langs, nil
}

func buildProfilePreflights(profiles []arrclient.ProfileLanguage, langs []string) []ProfilePreflight {
	out := make([]ProfilePreflight, 0, len(profiles))
	for _, p := range profiles {
		verdicts := make([]LanguageVerdict, 0, len(langs))
		for _, lang := range langs {
			v, r := foreigntitle.RadarrProfileVerdict(p.Language, lang)
			verdicts = append(verdicts, LanguageVerdict{
				Language: lang,
				Verdict:  v,
				Reason:   r,
			})
		}
		out = append(out, ProfilePreflight{
			ID:       p.ID,
			Name:     p.Name,
			Language: p.Language,
			Verdicts: verdicts,
		})
	}
	return out
}
