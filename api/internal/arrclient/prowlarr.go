// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var prowlarrPathRegex = regexp.MustCompile(`^/(\d{1,3})(?:/(?:api)?/?)?$`)

func matchHostAndPath(uBase, uProwlarr *url.URL) (string, bool) {
	if !strings.EqualFold(uBase.Scheme, uProwlarr.Scheme) || !strings.EqualFold(uBase.Host, uProwlarr.Host) {
		return "", false
	}
	pPath := strings.TrimRight(uProwlarr.Path, "/")
	bPath := uBase.Path
	if pPath != "" {
		if !strings.HasPrefix(bPath, pPath+"/") && bPath != pPath {
			return "", false
		}
		bPath = strings.TrimPrefix(bPath, pPath)
	}
	return bPath, true
}

// ProwlarrIndexerID extracts the indexer ID if baseURL targets prowlarrURL.
// The *arr indexer field baseUrl equals <prowlarrURL trimmed of trailing slash>/<id>/
// following Prowlarr regex \/(?<indexer>\d{1,3})(?:\/(?:api)?\/?)?$ (Applications/ApplicationBase.cs:19,
// Sonarr.cs:269 at Prowlarr v2.6.5.5623).
func ProwlarrIndexerID(baseURL, prowlarrURL string) (int, bool) {
	uBase, err := url.Parse(baseURL)
	if err != nil || uBase.Host == "" {
		return 0, false
	}
	uProwlarr, err := url.Parse(prowlarrURL)
	if err != nil || uProwlarr.Host == "" {
		return 0, false
	}
	bPath, ok := matchHostAndPath(uBase, uProwlarr)
	if !ok {
		return 0, false
	}
	m := prowlarrPathRegex.FindStringSubmatch(bPath)
	if len(m) < 2 {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
