// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DownloadClientKind identifies a supported torrent or Usenet client.
type DownloadClientKind string

// Supported download clients (goenvoy/downloadclient).
const (
	ClientQBittorrent  DownloadClientKind = "qbittorrent"
	ClientTransmission DownloadClientKind = "transmission"
	ClientDeluge       DownloadClientKind = "deluge"
	ClientRTorrent     DownloadClientKind = "rtorrent"
	ClientSABnzbd      DownloadClientKind = "sabnzbd"
	ClientNZBGet       DownloadClientKind = "nzbget"
)

// AllDownloadClientKinds lists every supported kind.
var AllDownloadClientKinds = []DownloadClientKind{
	ClientQBittorrent, ClientTransmission, ClientDeluge, ClientRTorrent, ClientSABnzbd, ClientNZBGet,
}

// ParseDownloadClientKind validates a kind string.
func ParseDownloadClientKind(s string) (DownloadClientKind, error) {
	for _, k := range AllDownloadClientKinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("%w: unknown download client kind %q", ErrInvalid, s)
}

// DownloadProtocol is the transport family a client serves.
type DownloadProtocol string

// Protocols.
const (
	ProtocolTorrent DownloadProtocol = "torrent"
	ProtocolUsenet  DownloadProtocol = "usenet"
)

// Protocol reports whether the kind downloads torrents or NZBs.
func (k DownloadClientKind) Protocol() DownloadProtocol {
	if k == ClientSABnzbd || k == ClientNZBGet {
		return ProtocolUsenet
	}
	return ProtocolTorrent
}

// DownloadClientSource records where a definition came from.
type DownloadClientSource string

// Sources.
const (
	ClientSourceManual     DownloadClientSource = "manual"
	ClientSourceDiscovered DownloadClientSource = "discovered"
)

// MaxActiveDownloadsLimit bounds max_active.
const MaxActiveDownloadsLimit = 1000

// DownloadClient is a torrent or Usenet client SnatchArr observes for backpressure. It is
// never used to add, remove or reorder downloads. InstanceID nil means the client applies
// to every instance; discovered clients always belong to the instance that listed them.
type DownloadClient struct {
	ID                 uuid.UUID
	InstanceID         *uuid.UUID
	Kind               DownloadClientKind
	Name               string
	BaseURL            string
	Username           string
	Enabled            bool
	Source             DownloadClientSource
	RemoteID           int
	MaxActive          int   // 0 = no limit
	BandwidthBudgetBPS int64 // 0 = no pacing
	LastCheckAt        *time.Time
	LastError          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ValidateDownloadClientInput checks the user-supplied fields of a download client.
func ValidateDownloadClientInput(kind DownloadClientKind, name, baseURL string, maxActive int, budgetBPS int64) error {
	if _, err := ParseDownloadClientKind(string(kind)); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return fmt.Errorf("%w: name must be 1-64 characters", ErrInvalid)
	}
	if _, err := NormalizeBaseURL(baseURL); err != nil {
		return err
	}
	if maxActive < 0 || maxActive > MaxActiveDownloadsLimit {
		return fmt.Errorf("%w: max_active must be between 0 and %d", ErrInvalid, MaxActiveDownloadsLimit)
	}
	if budgetBPS < 0 {
		return fmt.Errorf("%w: bandwidth_budget_bps must not be negative", ErrInvalid)
	}
	return nil
}
