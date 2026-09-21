// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/downloadclient/deluge"
	"github.com/golusoris/goenvoy/downloadclient/nzbget"
	"github.com/golusoris/goenvoy/downloadclient/qbit"
	"github.com/golusoris/goenvoy/downloadclient/rtorrent"
	"github.com/golusoris/goenvoy/downloadclient/sabnzbd"
	"github.com/golusoris/goenvoy/downloadclient/transmission"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Snapshotter observes one download client. It never returns an error: an unreachable
// client is a Snapshot with Reachable=false and the cause in Error.
type Snapshotter interface {
	Snapshot(ctx context.Context, c domain.DownloadClient, secret string) Snapshot
}

// observation is what every adapter extracts from its client's wire format.
type observation struct {
	Paused       bool
	Active       int
	Queued       int
	DownloadRate int64
	UploadRate   int64
	Version      string
}

// Goenvoy observes clients through goenvoy's typed download-client packages.
type Goenvoy struct {
	timeout   time.Duration
	userAgent string
	clk       clock.Clock
}

// ObserveTimeout bounds one observation.
const ObserveTimeout = 10 * time.Second

// NewGoenvoy builds the adapter set.
func NewGoenvoy(clk clock.Clock) *Goenvoy {
	return &Goenvoy{timeout: ObserveTimeout, userAgent: arrclient.DefaultOptions().UserAgent, clk: clk}
}

// Snapshot implements Snapshotter.
func (g *Goenvoy) Snapshot(ctx context.Context, c domain.DownloadClient, secret string) Snapshot {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	snap := Snapshot{ClientID: c.ID, Name: c.Name, Kind: c.Kind, CheckedAt: g.clk.Now()}
	obs, err := g.observe(ctx, c, secret)
	if err != nil {
		snap.Error = err.Error()
		return snap
	}
	snap.Reachable = true
	snap.Paused, snap.Active, snap.Queued = obs.Paused, obs.Active, obs.Queued
	snap.DownloadRate, snap.UploadRate, snap.Version = obs.DownloadRate, obs.UploadRate, obs.Version
	return snap
}

func (g *Goenvoy) observe(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	switch c.Kind {
	case domain.ClientQBittorrent:
		return g.qbittorrent(ctx, c, secret)
	case domain.ClientTransmission:
		return g.transmission(ctx, c, secret)
	case domain.ClientDeluge:
		return g.deluge(ctx, c, secret)
	case domain.ClientRTorrent:
		return g.rtorrent(ctx, c, secret)
	case domain.ClientSABnzbd:
		return g.sabnzbd(ctx, c, secret)
	case domain.ClientNZBGet:
		return g.nzbget(ctx, c, secret)
	default:
		return observation{}, fmt.Errorf("%w: unsupported download client kind %q", domain.ErrInvalid, c.Kind)
	}
}

func (g *Goenvoy) qbittorrent(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl := qbit.New(c.BaseURL, qbit.WithTimeout(g.timeout), qbit.WithUserAgent(g.userAgent))
	if c.Username != "" || secret != "" {
		if err := cl.Login(ctx, c.Username, secret); err != nil {
			return observation{}, fmt.Errorf("qbittorrent login: %w", err)
		}
	}
	version, err := cl.Version(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("qbittorrent version: %w", err)
	}
	torrents, err := cl.ListTorrents(ctx, &qbit.ListOptions{})
	if err != nil {
		return observation{}, fmt.Errorf("qbittorrent torrents: %w", err)
	}
	obs := observation{Version: version}
	for _, t := range torrents {
		obs.DownloadRate += t.DlSpeed
		obs.UploadRate += t.UpSpeed
		switch t.State {
		case "downloading", "metaDL", "forcedDL", "forcedMetaDL", "stalledDL", "checkingDL":
			obs.Active++
		case "queuedDL":
			obs.Queued++
		}
	}
	return obs, nil
}

// transmissionTarget splits a stored URL into goenvoy's base URL and RPC path: the *arr
// "urlBase" (default /transmission/) becomes "<urlBase>/rpc".
func transmissionTarget(baseURL string) (string, string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("transmission: %w", err)
	}
	path := strings.Trim(u.Path, "/")
	u.Path, u.RawPath = "", ""
	if path == "" {
		return u.String(), "", nil
	}
	if !strings.HasSuffix(path, "/rpc") && path != "rpc" {
		path += "/rpc"
	}
	return u.String(), "/" + path, nil
}

// Transmission torrent status codes (rpc-spec "status").
const (
	transmissionDownloadWait = 3
	transmissionDownloading  = 4
)

func (g *Goenvoy) transmission(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	base, rpcPath, err := transmissionTarget(c.BaseURL)
	if err != nil {
		return observation{}, err
	}
	opts := []transmission.Option{transmission.WithTimeout(g.timeout), transmission.WithUserAgent(g.userAgent)}
	if c.Username != "" {
		opts = append(opts, transmission.WithAuth(c.Username, secret))
	}
	if rpcPath != "" {
		opts = append(opts, transmission.WithRPCPath(rpcPath))
	}
	cl := transmission.New(base, opts...)
	session, err := cl.GetSession(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("transmission session: %w", err)
	}
	torrents, err := cl.GetTorrents(ctx, nil)
	if err != nil {
		return observation{}, fmt.Errorf("transmission torrents: %w", err)
	}
	obs := observation{Version: session.Version}
	for _, t := range torrents {
		obs.DownloadRate += t.RateDownload
		obs.UploadRate += t.RateUpload
		switch t.Status {
		case transmissionDownloading:
			obs.Active++
		case transmissionDownloadWait:
			obs.Queued++
		}
	}
	return obs, nil
}

func (g *Goenvoy) deluge(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl := deluge.New(c.BaseURL, deluge.WithTimeout(g.timeout), deluge.WithUserAgent(g.userAgent))
	if err := cl.Login(ctx, secret); err != nil {
		return observation{}, fmt.Errorf("deluge login: %w", err)
	}
	version, err := cl.GetVersion(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("deluge version: %w", err)
	}
	torrents, err := cl.GetTorrentsStatus(ctx, map[string]string{})
	if err != nil {
		return observation{}, fmt.Errorf("deluge torrents: %w", err)
	}
	obs := observation{Version: version}
	for _, t := range torrents {
		if t == nil {
			continue
		}
		obs.DownloadRate += t.DownloadPayloadRate
		obs.UploadRate += t.UploadPayloadRate
		switch t.State {
		case "Downloading":
			obs.Active++
		case "Queued":
			obs.Queued++
		}
	}
	return obs, nil
}

func (g *Goenvoy) rtorrent(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	opts := []rtorrent.Option{rtorrent.WithTimeout(g.timeout), rtorrent.WithUserAgent(g.userAgent)}
	if c.Username != "" {
		opts = append(opts, rtorrent.WithAuth(c.Username, secret))
	}
	cl := rtorrent.New(c.BaseURL, opts...)
	info, err := cl.GetSystemInfo(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("rtorrent system info: %w", err)
	}
	torrents, err := cl.GetTorrents(ctx, "main")
	if err != nil {
		return observation{}, fmt.Errorf("rtorrent torrents: %w", err)
	}
	obs := observation{Version: info.ClientVersion}
	for _, t := range torrents {
		obs.DownloadRate += t.DownRate
		obs.UploadRate += t.UpRate
		if t.IsOpen && t.IsActive && !t.IsComplete {
			obs.Active++
		}
	}
	return obs, nil
}

func (g *Goenvoy) sabnzbd(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl := sabnzbd.New(c.BaseURL, secret, sabnzbd.WithTimeout(g.timeout), sabnzbd.WithUserAgent(g.userAgent))
	version, err := cl.GetVersion(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("sabnzbd version: %w", err)
	}
	q, err := cl.GetQueue(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("sabnzbd queue: %w", err)
	}
	obs := observation{Version: version, Paused: q.Paused || q.PausedAll, DownloadRate: ParseSABSpeed(q.Speed)}
	for _, s := range q.Slots {
		switch s.Status {
		case "Downloading":
			obs.Active++
		case "Queued":
			obs.Queued++
		}
	}
	return obs, nil
}

// ParseSABSpeed converts SABnzbd's "1.2 M" style speed (per second, binary prefixes) to
// bytes per second. Unparseable input yields 0, which never causes backpressure.
func ParseSABSpeed(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	num, unit := s, ""
	if i := strings.LastIndexAny(s, "KMGTkmgt"); i >= 0 && i == len(s)-1 {
		num, unit = strings.TrimSpace(s[:i]), strings.ToUpper(s[i:])
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	mult := map[string]float64{"": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}[unit]
	return int64(math.Round(f * mult))
}

func (g *Goenvoy) nzbget(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl := nzbget.New(c.BaseURL, c.Username, secret, nzbget.WithTimeout(g.timeout), nzbget.WithUserAgent(g.userAgent))
	version, err := cl.GetVersion(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("nzbget version: %w", err)
	}
	st, err := cl.GetStatus(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("nzbget status: %w", err)
	}
	groups, err := cl.ListGroups(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("nzbget groups: %w", err)
	}
	obs := observation{Version: version, Paused: st.DownloadPaused, DownloadRate: st.DownloadRate}
	for _, grp := range groups {
		switch {
		case grp.ActiveDownloads > 0 || strings.HasPrefix(grp.Status, "DOWNLOADING"):
			obs.Active++
		case grp.Status == "QUEUED":
			obs.Queued++
		}
	}
	return obs, nil
}
