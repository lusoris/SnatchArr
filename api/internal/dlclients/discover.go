// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// implementations maps *arr "implementation" names to SnatchArr kinds.
var implementations = map[string]domain.DownloadClientKind{
	"QBittorrent":  domain.ClientQBittorrent,
	"Transmission": domain.ClientTransmission,
	"Deluge":       domain.ClientDeluge,
	"RTorrent":     domain.ClientRTorrent,
	"Sabnzbd":      domain.ClientSABnzbd,
	"Nzbget":       domain.ClientNZBGet,
}

// Discovered is one *arr download client definition mapped onto SnatchArr's model.
type Discovered struct {
	Input    Input
	RemoteID int
	// RedactedSecret is set when the *arr masked the password or API key, so the user has
	// to enter it by hand before the client can be observed.
	RedactedSecret bool
}

// FromProvider maps one *arr provider resource. ok is false for implementations SnatchArr
// does not observe (blackholes, Aria2, Flood, ...).
func FromProvider(instanceID uuid.UUID, r arr.ProviderResource) (Discovered, bool) {
	kind, ok := implementations[r.Implementation]
	if !ok {
		return Discovered{}, false
	}
	f := fieldMap(r.Fields)
	category := ""
	if priorityKind(kind) {
		category = providerCategory(f)
	}
	secret := f.str("password")
	if kind == domain.ClientSABnzbd {
		secret = f.str("apiKey")
	}
	d := Discovered{
		Input: Input{
			InstanceID: &instanceID, Kind: kind, Name: r.Name, BaseURL: providerURL(kind, f),
			Category: category,
			Username: f.str("username"), Secret: secret, Enabled: r.Enable == nil || *r.Enable,
		},
		RemoteID:       r.ID,
		RedactedSecret: isRedacted(secret),
	}
	if d.RedactedSecret {
		d.Input.Secret = ""
	}
	return d, true
}

// providerCategory accepts only the download category, never the post-import category.
// Upstream checked 2026-11-01: src/NzbDrone.Core/Download/Clients/{client}/{client}Settings.cs,
// classes QBittorrentSettings, NzbgetSettings and SabnzbdSettings in each repository:
// https://github.com/Radarr/Radarr/tree/develop/src/NzbDrone.Core/Download/Clients (MovieCategory)
// https://github.com/Sonarr/Sonarr/tree/develop/src/NzbDrone.Core/Download/Clients (TvCategory)
// https://github.com/Lidarr/Lidarr/tree/develop/src/NzbDrone.Core/Download/Clients (MusicCategory)
// https://github.com/Readarr/Readarr/tree/develop/src/NzbDrone.Core/Download/Clients (MusicCategory)
// https://github.com/Whisparr/Whisparr/tree/develop/src/NzbDrone.Core/Download/Clients (TvCategory)
// https://github.com/Whisparr/Whisparr/tree/eros/src/NzbDrone.Core/Download/Clients (MovieCategory).
// ProviderResource field names use camelCase. Ambiguous definitions fail attribution closed.
func providerCategory(f fields) string {
	category, found := "", false
	for _, key := range []string{"movieCategory", "tvCategory", "musicCategory"} {
		if _, ok := f[key]; !ok {
			continue
		}
		if found {
			return ""
		}
		category, _ = f[key].(string)
		found = true
	}
	return category
}

// isRedacted recognises the mask *arr apps substitute for privacy fields.
func isRedacted(secret string) bool {
	return len(secret) >= 4 && strings.Trim(secret, "*") == ""
}

type fields map[string]any

func fieldMap(list []arr.ProviderField) fields {
	m := make(fields, len(list))
	for _, f := range list {
		m[f.Name] = f.Value
	}
	return m
}

func (f fields) str(name string) string {
	switch v := f[name].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func (f fields) num(name string) int {
	switch v := f[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func (f fields) boolean(name string) bool {
	v, _ := f[name].(bool)
	return v
}

// providerURL rebuilds the client URL from the *arr host/port/useSsl/urlBase fields.
// rTorrent's urlBase is its XML-RPC endpoint (default RPC2); Transmission's is the web
// path (default /transmission/), which the adapter turns into <urlBase>/rpc.
func providerURL(kind domain.DownloadClientKind, f fields) string {
	scheme := "http"
	if f.boolean("useSsl") {
		scheme = "https"
	}
	base := strings.Trim(f.str("urlBase"), "/")
	switch kind {
	case domain.ClientRTorrent:
		if base == "" {
			base = "RPC2"
		}
	case domain.ClientTransmission:
		if base == "" {
			base = "transmission"
		}
	case domain.ClientQBittorrent, domain.ClientDeluge, domain.ClientSABnzbd, domain.ClientNZBGet:
	}
	host := f.str("host")
	if port := f.num("port"); port > 0 {
		host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	u := url.URL{Scheme: scheme, Host: host}
	if base != "" {
		u.Path = "/" + base
	}
	return u.String()
}
