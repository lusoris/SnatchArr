// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type wireTrackRule struct {
	Languages *[]string `json:"languages"`
}

type wireRule struct {
	Track *wireTrackRule `json:"track"`
}

// DecodeRuleJSON strictly decodes and validates a Rule JSON document against torcul/job-request/1.
func DecodeRuleJSON(raw []byte) (Rule, error) {
	trimmed := bytes.Trim(raw, " \t\r\n")
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return Rule{}, fmt.Errorf("%w: rule must be a JSON object", domain.ErrInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var w wireRule
	if err := dec.Decode(&w); err != nil {
		return Rule{}, fmt.Errorf("%w: decode rule: %w", domain.ErrInvalid, err)
	}
	if len(bytes.Trim(trimmed[dec.InputOffset():], " \t\r\n")) != 0 {
		return Rule{}, fmt.Errorf("%w: trailing data after rule JSON", domain.ErrInvalid)
	}
	if w.Track == nil {
		return Rule{}, fmt.Errorf("%w: rule.track is required", domain.ErrInvalid)
	}
	if w.Track.Languages == nil {
		return Rule{}, fmt.Errorf("%w: rule.track.languages is required", domain.ErrInvalid)
	}
	out := Rule{Track: TrackRule{Languages: *w.Track.Languages}}
	if err := ValidateRule(out); err != nil {
		return Rule{}, err
	}
	return CloneRule(out), nil
}

// ValidateRule enforces torcul/job-request/1 Rule and TrackRule constraints.
func ValidateRule(r Rule) error {
	langs := r.Track.Languages
	if len(langs) == 0 {
		return fmt.Errorf("%w: rule.track.languages must contain at least 1 entry", domain.ErrInvalid)
	}
	if len(langs) > MaxRuleLanguages {
		return fmt.Errorf("%w: rule.track.languages exceeds maximum of %d entries", domain.ErrInvalid, MaxRuleLanguages)
	}
	seen := make(map[string]struct{}, len(langs))
	for _, code := range langs {
		if _, err := parseISO639(code); err != nil {
			return fmt.Errorf("rule.track.languages: %w", err)
		}
		if _, dup := seen[code]; dup {
			return fmt.Errorf("%w: duplicate language %q in rule.track.languages", domain.ErrInvalid, code)
		}
		seen[code] = struct{}{}
	}
	return nil
}

func isLowerASCIIAlpha(raw string) bool {
	if len(raw) < 2 || len(raw) > 3 {
		return false
	}
	for i := range len(raw) {
		if raw[i] < 'a' || raw[i] > 'z' {
			return false
		}
	}
	return true
}

func parseISO639(raw string) (string, error) {
	if !isLowerASCIIAlpha(raw) {
		return "", fmt.Errorf("%w: invalid ISO 639 code %q", domain.ErrInvalid, raw)
	}
	base, err := language.ParseBase(raw)
	if err != nil || base.IsPrivateUse() {
		return "", fmt.Errorf("%w: unknown ISO 639 code %q", domain.ErrInvalid, raw)
	}
	iso3 := base.ISO3()
	if iso3 == "" || iso3 == "und" {
		return "", fmt.Errorf("%w: undetermined ISO 639 code %q is not allowed", domain.ErrInvalid, raw)
	}
	return iso3, nil
}

func validateInstanceUUID(id *uuid.UUID) (uuid.UUID, error) {
	if id == nil || *id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: instance_id is required", domain.ErrInvalid)
	}
	return *id, nil
}

func validateItemID(raw *string) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("%w: item_id is required", domain.ErrInvalid)
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" || trimmed != *raw {
		return "", fmt.Errorf("%w: item_id must be non-empty without surrounding whitespace", domain.ErrInvalid)
	}
	if utf8.RuneCountInString(trimmed) > MaxItemIDLen {
		return "", fmt.Errorf("%w: item_id exceeds maximum length of %d", domain.ErrInvalid, MaxItemIDLen)
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: item_id must not contain control characters", domain.ErrInvalid)
		}
	}
	return trimmed, nil
}

func hasDotSpaceSegment(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if seg != "" && strings.Trim(seg, ". ") == "" && strings.Contains(seg, " ") {
			return true
		}
	}
	return false
}

func isCleanPOSIXRoot(p string) bool {
	return !strings.ContainsRune(p, '\x00') && path.IsAbs(p) && path.Clean(p) == p
}

func validateRootPath(raw *string) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("%w: root_path is required", domain.ErrInvalid)
	}
	p := *raw
	if p == "" || len(p) > MaxRootPathBytes {
		return "", fmt.Errorf("%w: root_path must be 1..%d bytes", domain.ErrInvalid, MaxRootPathBytes)
	}
	if !isCleanPOSIXRoot(p) || hasDotSpaceSegment(p) {
		return "", fmt.Errorf("%w: root_path %q must be a clean POSIX absolute path", domain.ErrInvalid, p)
	}
	return p, nil
}

type normalizedInput struct {
	scope            Scope
	instanceID       *uuid.UUID
	itemID           *string
	userLanguage     *string
	userLanguageCode *string
	rootPath         *string
	mediaType        *string
	rule             Rule
}

func forbidOtherTargets(scope Scope, ok bool) error {
	if !ok {
		return fmt.Errorf("%w: scope %s forbids target fields from other scopes", domain.ErrInvalid, scope)
	}
	return nil
}

func normalizeInput(in RuleInput) (normalizedInput, error) {
	if !in.Scope.Valid() {
		return normalizedInput{}, fmt.Errorf("%w: invalid scope", domain.ErrInvalid)
	}
	if err := ValidateRule(in.Rule); err != nil {
		return normalizedInput{}, err
	}
	out := normalizedInput{scope: in.Scope, rule: CloneRule(in.Rule)}
	switch in.Scope {
	case ScopeItemOverride:
		return normalizeItemOverride(in, out)
	case ScopeUserLanguage:
		return normalizeUserLanguage(in, out)
	case ScopeLibrary:
		return normalizeLibrary(in, out)
	case ScopeInstance:
		return normalizeInstance(in, out)
	case ScopeMediaType:
		return normalizeMediaType(in, out)
	case ScopeGlobalDefault:
		return normalizeGlobalDefault(in, out)
	default:
		return normalizedInput{}, fmt.Errorf("%w: invalid scope", domain.ErrInvalid)
	}
}

func normalizeItemOverride(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.UserLanguage == nil && in.RootPath == nil && in.MediaType == nil); err != nil {
		return normalizedInput{}, err
	}
	instID, err := validateInstanceUUID(in.InstanceID)
	if err != nil {
		return normalizedInput{}, err
	}
	itemID, err := validateItemID(in.ItemID)
	if err != nil {
		return normalizedInput{}, err
	}
	out.instanceID = &instID
	out.itemID = &itemID
	return out, nil
}

func normalizeUserLanguage(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.InstanceID == nil && in.ItemID == nil && in.RootPath == nil && in.MediaType == nil); err != nil {
		return normalizedInput{}, err
	}
	if in.UserLanguage == nil {
		return normalizedInput{}, fmt.Errorf("%w: user_language is required", domain.ErrInvalid)
	}
	iso3, err := parseISO639(*in.UserLanguage)
	if err != nil {
		return normalizedInput{}, fmt.Errorf("user_language: %w", err)
	}
	lang := *in.UserLanguage
	out.userLanguage = &lang
	out.userLanguageCode = &iso3
	return out, nil
}

func normalizeLibrary(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.ItemID == nil && in.UserLanguage == nil && in.MediaType == nil); err != nil {
		return normalizedInput{}, err
	}
	instID, err := validateInstanceUUID(in.InstanceID)
	if err != nil {
		return normalizedInput{}, err
	}
	rootPath, err := validateRootPath(in.RootPath)
	if err != nil {
		return normalizedInput{}, err
	}
	out.instanceID = &instID
	out.rootPath = &rootPath
	return out, nil
}

func normalizeInstance(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.ItemID == nil && in.UserLanguage == nil && in.RootPath == nil && in.MediaType == nil); err != nil {
		return normalizedInput{}, err
	}
	instID, err := validateInstanceUUID(in.InstanceID)
	if err != nil {
		return normalizedInput{}, err
	}
	out.instanceID = &instID
	return out, nil
}

func normalizeMediaType(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.InstanceID == nil && in.ItemID == nil && in.UserLanguage == nil && in.RootPath == nil); err != nil {
		return normalizedInput{}, err
	}
	if in.MediaType == nil {
		return normalizedInput{}, fmt.Errorf("%w: media_type is required", domain.ErrInvalid)
	}
	mt, err := ParseMediaType(string(*in.MediaType))
	if err != nil {
		return normalizedInput{}, err
	}
	mtStr := string(mt)
	out.mediaType = &mtStr
	return out, nil
}

func normalizeGlobalDefault(in RuleInput, out normalizedInput) (normalizedInput, error) {
	if err := forbidOtherTargets(in.Scope, in.InstanceID == nil && in.ItemID == nil && in.UserLanguage == nil && in.RootPath == nil && in.MediaType == nil); err != nil {
		return normalizedInput{}, err
	}
	return out, nil
}

type normalizedResolve struct {
	instanceID       *uuid.UUID
	itemID           *string
	userLanguageCode *string
	rootPath         *string
	mediaType        *string
}

func normalizeResolveQuery(q ResolveQuery) (normalizedResolve, error) {
	var out normalizedResolve
	if q.InstanceID != nil {
		id, err := validateInstanceUUID(q.InstanceID)
		if err != nil {
			return normalizedResolve{}, err
		}
		out.instanceID = &id
	}
	if err := normalizeResolveInstanceScoped(q, &out); err != nil {
		return normalizedResolve{}, err
	}
	if q.RequesterLanguage != nil {
		iso3, err := parseISO639(*q.RequesterLanguage)
		if err != nil {
			return normalizedResolve{}, fmt.Errorf("requester_language: %w", err)
		}
		out.userLanguageCode = &iso3
	}
	if q.MediaType != nil {
		mt, err := ParseMediaType(string(*q.MediaType))
		if err != nil {
			return normalizedResolve{}, err
		}
		mtStr := string(mt)
		out.mediaType = &mtStr
	}
	return out, nil
}

func normalizeResolveInstanceScoped(q ResolveQuery, out *normalizedResolve) error {
	if q.ItemID != nil {
		if out.instanceID == nil {
			return fmt.Errorf("%w: instance_id is required when item_id is set", domain.ErrInvalid)
		}
		itemID, err := validateItemID(q.ItemID)
		if err != nil {
			return err
		}
		out.itemID = &itemID
	}
	if q.RootPath != nil {
		if out.instanceID == nil {
			return fmt.Errorf("%w: instance_id is required when root_path is set", domain.ErrInvalid)
		}
		rootPath, err := validateRootPath(q.RootPath)
		if err != nil {
			return err
		}
		out.rootPath = &rootPath
	}
	return nil
}
