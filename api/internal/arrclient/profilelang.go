// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Quality profile language queries for Radarr instances.
// goenvoy arr/v2 v2.6.2 arr.QualityProfile has no language field, so the minimal decode
// of {id, name, language{id,name}} is required directly via BaseClient.Get.

package arrclient

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

// maxProfiles caps the number of quality profiles parsed from an *arr instance response.
const maxProfiles = 1000

// maxProfileBody bounds the upstream response before goenvoy buffers or decodes it.
const maxProfileBody = 8 << 20

// ProfileLanguage represents a quality profile and its configured language in Radarr.
type ProfileLanguage struct {
	ID       int32
	Name     string
	Language *foreigntitle.RadarrLanguage
}

// ProfileLanguager lists quality profiles and their configured language for an *arr instance.
type ProfileLanguager interface {
	ProfileLanguages(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]ProfileLanguage, error)
}

type rawLanguage struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type rawQualityProfile struct {
	ID       int          `json:"id"`
	Name     string       `json:"name"`
	Language *rawLanguage `json:"language"`
}

// ProfileLanguages queries Radarr for quality profiles and decodes their assigned language.
func (p *GoenvoyProber) ProfileLanguages(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]ProfileLanguage, error) {
	if kind != domain.KindRadarr {
		return nil, fmt.Errorf("%w: profile languages only supported for %s", domain.ErrInvalid, domain.KindRadarr)
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	c, err := p.opts.BaseCapped(baseURL, apiKey, maxProfileBody)
	if err != nil {
		return nil, fmt.Errorf("arrclient: %w", err)
	}
	var list []rawQualityProfile
	if err := c.Get(ctx, "/api/v3/qualityprofile", &list); err != nil {
		return nil, fmt.Errorf("%w: quality profiles: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	if len(list) > maxProfiles {
		return nil, fmt.Errorf("%w: quality profiles count %d exceeds maximum %d", domain.ErrUnreachable, len(list), maxProfiles)
	}
	out := filterAndConvertProfiles(list)
	sortProfileLanguages(out)
	return out, nil
}

// filterAndConvertProfiles keeps profiles within 1..MaxInt32 and maps the language field.
func filterAndConvertProfiles(list []rawQualityProfile) []ProfileLanguage {
	limit := min(len(list), maxProfiles)
	out := make([]ProfileLanguage, 0, limit)
	for i := range limit {
		item := list[i]
		if item.ID < 1 || item.ID > math.MaxInt32 {
			continue
		}
		var lang *foreigntitle.RadarrLanguage
		if item.Language != nil && item.Language.ID >= math.MinInt32 && item.Language.ID <= math.MaxInt32 {
			lang = &foreigntitle.RadarrLanguage{
				ID:   item.Language.ID,
				Name: item.Language.Name,
			}
		}
		out = append(out, ProfileLanguage{
			ID:       int32(item.ID), // #nosec G115 -- bounded to 1..MaxInt32 above
			Name:     item.Name,
			Language: lang,
		})
	}
	return out
}

// sortProfileLanguages orders profiles case-insensitively by name, then by ID.
func sortProfileLanguages(list []ProfileLanguage) {
	slices.SortFunc(list, func(a, b ProfileLanguage) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}
