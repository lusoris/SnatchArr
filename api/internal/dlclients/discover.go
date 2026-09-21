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
	secret := f.str("password")
	if kind == domain.ClientSABnzbd {
		secret = f.str("apiKey")
	}
	d := Discovered{
		Input: Input{
			InstanceID: &instanceID, Kind: kind, Name: r.Name, BaseURL: providerURL(kind, f),
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
