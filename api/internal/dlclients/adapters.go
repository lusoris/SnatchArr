// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/downloadclient/deluge/v2"
	"github.com/golusoris/goenvoy/downloadclient/nzbget/v2"
	"github.com/golusoris/goenvoy/downloadclient/qbit/v2"
	"github.com/golusoris/goenvoy/downloadclient/rtorrent/v2"
	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"
	"github.com/golusoris/goenvoy/downloadclient/transmission/v2"

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

// dlClass categorizes a download item's operational state for capacity accounting.
type dlClass uint8

const (
	dlClassNone dlClass = iota
	dlClassActive
	dlClassQueued
)

// ErrDelugeNoDaemon reports a Deluge web UI that is not connected to a daemon. SnatchArr only
// observes, so it does not connect one; the connection manager of the web UI does.
var ErrDelugeNoDaemon = errors.New("deluge: the web UI is not connected to a daemon; connect one in its connection manager")

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

// qBittorrent release-5.3.0 state classification.
// Source: qbittorrent/qBittorrent@release-5.3.0 (commit 9213075dd5f7b352208361c6a84c1c115efc898e)
// - src/base/bittorrent/torrent.h:82-110 (enum class TorrentState)
// - src/webui/api/serialize/serialize_torrent.cpp:44-87 (torrentStateToString)
// - src/base/bittorrent/torrentimpl.cpp:1187-1347 (updateState, isActive)
//
// | State String         | Classification | Reason (file:line)                                         |
// | :------------------- | :------------- | :--------------------------------------------------------- |
// | "downloading"        | active         | Actively transferring payload (torrentimpl.cpp:1342-1343)   |
// | "forcedDL"           | active         | Force-started download (torrentimpl.cpp:1340-1341)          |
// | "stalledDL"          | queued         | Started slot with 0 B/s payload rate (torrentimpl.cpp:1344) |
// | "metaDL"             | queued         | Fetching .torrent metadata (torrentimpl.cpp:1307-1314)      |
// | "forcedMetaDL"       | queued         | Force-fetching .torrent metadata (torrentimpl.cpp:1314)     |
// | "checkingDL"         | queued         | Checking piece hashes before download (torrentimpl:1316)   |
// | "checkingResumeData" | queued         | Checking fastresume data on start (torrentimpl.cpp:1291)   |
// | "queuedDL"           | queued         | Waiting in queue for download slot (torrentimpl.cpp:1311)   |
// | "allocating"         | queued         | Pre-allocating disk space (legacy 4.x / libtorrent 1.2)    |
// | "stoppedDL"          | none           | User-stopped/paused unfinished download                    |
// | "pausedDL"           | none           | Legacy 4.x name for stoppedDL                              |
// | "moving"             | none           | Moving torrent storage on disk                             |
// | "uploading"          | none           | Completed torrent seeding                                  |
// | "stalledUP"          | none           | Completed torrent seeding with 0 B/s                       |
// | "queuedUP"           | none           | Completed torrent queued for seed slot                     |
// | "forcedUP"           | none           | Completed torrent force-seeding                            |
// | "checkingUP"         | none           | Completed torrent checking piece hashes                    |
// | "stoppedUP"          | none           | Completed torrent stopped/paused                           |
// | "pausedUP"           | none           | Legacy 4.x name for stoppedUP                              |
// | "error"              | none           | Error state                                                |
// | "missingFiles"       | none           | Missing files on disk                                      |
// | "unknown"            | none           | Fallback / uninitialized state                             |.
var qbitStateClasses = map[string]dlClass{
	"downloading":        dlClassActive,
	"forcedDL":           dlClassActive,
	"stalledDL":          dlClassQueued,
	"metaDL":             dlClassQueued,
	"forcedMetaDL":       dlClassQueued,
	"checkingDL":         dlClassQueued,
	"checkingResumeData": dlClassQueued,
	"queuedDL":           dlClassQueued,
	"allocating":         dlClassQueued,
}

func classifyQbit(state string) dlClass {
	return qbitStateClasses[state]
}

func (g *Goenvoy) qbittorrent(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl, err := qbit.New(c.BaseURL, qbit.WithTimeout(g.timeout), qbit.WithUserAgent(g.userAgent))
	if err != nil {
		return observation{}, fmt.Errorf("qbittorrent client: %w", err)
	}
	version, err := qbittorrentVersion(ctx, cl, c.Username, secret)
	if err != nil {
		return observation{}, err
	}
	torrents, err := cl.ListTorrents(ctx, &qbit.ListOptions{})
	if err != nil {
		return observation{}, fmt.Errorf("qbittorrent torrents: %w", err)
	}
	obs := observation{Version: version}
	for _, t := range torrents {
		obs.DownloadRate += t.DlSpeed
		obs.UploadRate += t.UpSpeed
		switch classifyQbit(t.State) {
		case dlClassActive:
			obs.Active++
		case dlClassQueued:
			obs.Queued++
		case dlClassNone:
		}
	}
	return obs, nil
}

func qbittorrentVersion(ctx context.Context, cl *qbit.Client, username, secret string) (string, error) {
	hasCredentials := username != "" || secret != ""
	if hasCredentials {
		if err := cl.Login(ctx, username, secret); err != nil {
			return "", qbittorrentLoginError(err)
		}
	}
	version, err := cl.Version(ctx)
	if err != nil {
		// goenvoy v2.0.0 ignores login's 200 Fails. body; the first authenticated call detects it.
		if apiErr, ok := errors.AsType[*qbit.APIError](err); hasCredentials && ok && apiErr.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("qbittorrent login failed: wrong username or password: %w", err)
		}
		return "", fmt.Errorf("qbittorrent version: %w", err)
	}
	return version, nil
}

func qbittorrentLoginError(err error) error {
	if apiErr, ok := errors.AsType[*qbit.APIError](err); ok {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("qBittorrent refused the request (HTTP 401): its WebUI Host header validation rejects this address; use the host and port qBittorrent serves on, or add this host to its WebUI server domains: %w", err)
		case http.StatusForbidden:
			return fmt.Errorf("qBittorrent banned this IP after too many failed logins: %w", err)
		}
	}
	return fmt.Errorf("qbittorrent login: %w", err)
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

// Transmission 4.1.4 torrent status codes (rpc-spec "status", tr_torrent_activity).
// Source: transmission/transmission@4.1.4 (commit a6d907d9ea2313143af714d7830feb6c1b60b84c)
// - libtransmission/transmission.h:1434-1445 (enum tr_torrent_activity : uint8_t)
// - libtransmission/torrent.cc:616-645,1350-1370 (tr_torrent::start, tr_torrent::activity)
//
// | Status Int | Enum Constant          | Classification | Reason (file:line)                                         |
// | :--------- | :--------------------- | :------------- | :--------------------------------------------------------- |
// | 4          | TR_STATUS_DOWNLOAD     | active/queued  | Active when Error==0 && !IsStalled; queued when IsStalled   |
// | 3          | TR_STATUS_DOWNLOAD_WAIT| queued         | Waiting in queue for download slot (torrent.cc:623)        |
// | 1          | TR_STATUS_CHECK_WAIT   | queued/none    | Queued if LeftUntilDone>0 && !IsFinished (torrent.cc:632)  |
// | 2          | TR_STATUS_CHECK        | queued/none    | Queued if LeftUntilDone>0 && !IsFinished (torrent.cc:631)  |
// | 0          | TR_STATUS_STOPPED      | none           | Stopped/paused (torrent.cc:637)                            |
// | 5          | TR_STATUS_SEED_WAIT    | none           | Seeding queue wait (torrent.cc:622)                        |
// | 6          | TR_STATUS_SEED         | none           | Seeding (torrent.cc:618)                                   |.
const (
	transmissionCheckWait    = 1
	transmissionCheck        = 2
	transmissionDownloadWait = 3
	transmissionDownloading  = 4
)

func classifyTransmission(t transmission.Torrent) dlClass {
	if t.Error != 0 {
		return dlClassNone
	}
	switch t.Status {
	case transmissionDownloading:
		if t.IsStalled {
			return dlClassQueued
		}
		return dlClassActive
	case transmissionDownloadWait:
		return dlClassQueued
	case transmissionCheckWait, transmissionCheck:
		if t.LeftUntilDone > 0 && !t.IsFinished {
			return dlClassQueued
		}
		return dlClassNone
	default:
		return dlClassNone
	}
}

func transmissionClient(baseURL, username, secret string, timeout time.Duration, userAgent string) (*transmission.Client, error) {
	base, rpcPath, err := transmissionTarget(baseURL)
	if err != nil {
		return nil, err
	}
	opts := []transmission.Option{transmission.WithTimeout(timeout), transmission.WithUserAgent(userAgent)}
	if username != "" {
		opts = append(opts, transmission.WithAuth(username, secret))
	}
	if rpcPath != "" {
		opts = append(opts, transmission.WithRPCPath(rpcPath))
	}
	cl, err := transmission.New(base, opts...)
	if err != nil {
		return nil, fmt.Errorf("transmission client: %w", err)
	}
	return cl, nil
}

func (g *Goenvoy) transmission(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl, err := transmissionClient(c.BaseURL, c.Username, secret, g.timeout, g.userAgent)
	if err != nil {
		return observation{}, err
	}
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
		switch classifyTransmission(t) {
		case dlClassActive:
			obs.Active++
		case dlClassQueued:
			obs.Queued++
		case dlClassNone:
		}
	}
	return obs, nil
}

// delugeSession logs in to the web UI and checks that it is connected to a daemon.
func (g *Goenvoy) delugeSession(ctx context.Context, c domain.DownloadClient, secret string) (*deluge.Client, error) {
	cl, err := deluge.New(c.BaseURL, deluge.WithTimeout(g.timeout), deluge.WithUserAgent(g.userAgent))
	if err != nil {
		return nil, fmt.Errorf("deluge client: %w", err)
	}
	if err = cl.Login(ctx, secret); err != nil {
		return nil, fmt.Errorf("deluge login: %w", err)
	}
	// Without a daemon the web UI answers every daemon and core method with "Unknown method".
	connected, err := cl.Connected(ctx)
	if err != nil {
		return nil, fmt.Errorf("deluge connection state: %w", err)
	}
	if !connected {
		return nil, ErrDelugeNoDaemon
	}
	return cl, nil
}

// Deluge 2.2.0 state classification.
// Source: deluge-torrent/deluge@deluge-2.2.0 (commit e9777eaabc8698473a05d8c02624f462bdc5af61)
// - deluge/common.py:73-82 (TORRENT_STATE)
// - deluge/core/torrent.py:39-48 (LT_TORRENT_STATE_MAP)
// - deluge/core/torrent.py:644-671 (Torrent.update_state)
//
// | State String | Classification | Reason (file:line)                                         |
// | :----------- | :------------- | :--------------------------------------------------------- |
// | "Downloading"| active         | Transferring payload or metadata (core/torrent.py:42-43)    |
// | "Queued"     | queued/none    | Queued if !IsFinished && !IsSeed (core/torrent.py:664-665) |
// | "Checking"   | queued/none    | Queued if !IsFinished && !IsSeed (core/torrent.py:40-41)   |
// | "Allocating" | queued/none    | Queued if !IsFinished && !IsSeed (core/torrent.py:46)      |
// | "Paused"     | none           | User or session pause (core/torrent.py:666-667)           |
// | "Moving"     | none           | Storage relocation (core/torrent.py:657-658)               |
// | "Seeding"    | none           | Completed seeding (core/torrent.py:44-45)                  |
// | "Error"      | none           | Error state (core/torrent.py:649-656)                      |.
func classifyDeluge(t *deluge.Torrent) dlClass {
	if t == nil {
		return dlClassNone
	}
	switch t.State {
	case "Downloading":
		return dlClassActive
	case "Queued", "Checking", "Allocating":
		if !t.IsFinished && !t.IsSeed {
			return dlClassQueued
		}
		return dlClassNone
	default:
		return dlClassNone
	}
}

func (g *Goenvoy) deluge(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl, err := g.delugeSession(ctx, c, secret)
	if err != nil {
		return observation{}, err
	}
	torrents, err := cl.GetTorrentsStatus(ctx, map[string]string{})
	if err != nil {
		return observation{}, fmt.Errorf("deluge torrents: %w", err)
	}
	// goenvoy asks for the version with daemon.info, which Deluge 2 does not serve
	// (golusoris/goenvoy#101). The version is informational, so a daemon that answers the
	// torrent list is reachable and its version stays unknown.
	version, err := cl.GetVersion(ctx)
	if err != nil {
		version = ""
	}
	obs := observation{Version: version}
	for _, t := range torrents {
		if t == nil {
			continue
		}
		obs.DownloadRate += t.DownloadPayloadRate
		obs.UploadRate += t.UploadPayloadRate
		switch classifyDeluge(t) {
		case dlClassActive:
			obs.Active++
		case dlClassQueued:
			obs.Queued++
		case dlClassNone:
		}
	}
	return obs, nil
}

// rTorrent v0.16.26 state classification.
// Source: rakshasa/rtorrent@v0.16.26 (commit 62899bd053c39b70380464e8f64dbcfce92579aa)
// - src/command_download.cc:696-745,826-832 (d.is_open, d.is_active, d.state, d.complete, d.hashing)
// - src/core/download.h:28-31,41-53 (is_open, is_active, is_done, is_hash_checking)
// - src/core/download_list.cc:228-553,659-700 (open, close, resume, pause, hash_queue)
// - src/core/manager.cc:390-455 (Manager::receive_hashing_changed)
//
// | Complete | Open | Active | HashChecking | Classification | Reason (file:line)                                         |
// | :------- | :--- | :----- | :----------- | :------------- | :--------------------------------------------------------- |
// | false    | true | true   | false        | active         | Incomplete running peer transfers (download.h:44)          |
// | false    | any  | false  | true         | queued         | Incomplete actively verifying piece hashes (manager.cc:438)|
// | false    | false| false  | false        | none           | Closed/stopped (see note below)                            |
// | false    | true | false  | false        | none           | Paused in memory (download_list.cc:487-535)                |
// | true     | any  | any    | any          | none           | Completed / seeding / complete check                       |
//
// goenvoy rtorrent/v2@v2.0.1 does not fetch d.hashing, so a torrent waiting in the hash
// queue (closed, d.hashing != 0) looks like a stopped one and counts as none until goenvoy
// reads that field.
func classifyRTorrent(t rtorrent.Torrent) dlClass {
	if t.IsComplete {
		return dlClassNone
	}
	if t.IsOpen && t.IsActive {
		return dlClassActive
	}
	if t.IsHashChecking {
		return dlClassQueued
	}
	return dlClassNone
}

func (g *Goenvoy) rtorrent(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	opts := []rtorrent.Option{rtorrent.WithTimeout(g.timeout), rtorrent.WithUserAgent(g.userAgent)}
	if c.Username != "" {
		opts = append(opts, rtorrent.WithAuth(c.Username, secret))
	}
	cl, err := rtorrent.New(c.BaseURL, opts...)
	if err != nil {
		return observation{}, fmt.Errorf("rtorrent client: %w", err)
	}
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
		switch classifyRTorrent(t) {
		case dlClassActive:
			obs.Active++
		case dlClassQueued:
			obs.Queued++
		case dlClassNone:
		}
	}
	return obs, nil
}

// SABnzbd 5.1.3 queue slot classification.
// Source: sabnzbd/sabnzbd@5.1.3 (commit a647f73426cb70ad60ae7964b96c2d5e939b4727)
// - sabnzbd/constants.py:167-185 (class Status)
// - sabnzbd/api.py:1679-1748 (build_queue, queue & slot status)
// - sabnzbd/nzb/object.py:334-337 (pre_check status)
// - sabnzbd/nzbqueue.py:718-732,782-788 (get_articles, end_job)
//
// | Status String | Classification   | Reason (file:line)                                         |
// | :------------ | :--------------- | :--------------------------------------------------------- |
// | "Downloading" | active/queued    | SAB marks ALL unpaused waiting slots "Downloading"         |
// |               |                  | (api.py:1713-1725); head slot transfers, rest queued.     |
// | "Fetching"    | active/queued    | Extra par2 download (postproc.py:892); head or queued.    |
// | "Checking"    | queued           | Pre-check running or waiting (nzb/object.py:334-337)       |
// | "Grabbing"    | queued           | Fetching .nzb from URL (urlgrabber.py:447)                 |
// | "Propagating" | queued           | Propagation delay timer (api.py:1714-1715)                 |
// | "Queued"      | queued           | Emitted when downloader paused (api.py:1720-1724)          |
// | "Paused"      | none             | Individually paused slot                                   |
// | "Deleted"     | none             | Marked deleted                                             |
// | "Idle"        | none             | Queue status header only                                   |
// | "QuickCheck"  | none             | Post-processing quick verification                         |
// | "Verifying"   | none             | Post-processing par2 verification                          |
// | "Repairing"   | none             | Post-processing par2 repair                                |
// | "Extracting"  | none             | Post-processing archive unpack                             |
// | "Moving"      | none             | Post-processing move to destination                         |
// | "Running"     | none             | Post-processing script running                             |
// | "Completed"   | none             | Post-processing finished                                    |
// | "Failed"      | none             | Post-processing failed                                     |.
var sabSlotClasses = map[string]dlClass{
	"Downloading": dlClassActive,
	"Fetching":    dlClassActive,
	"Checking":    dlClassQueued,
	"Grabbing":    dlClassQueued,
	"Propagating": dlClassQueued,
	"Queued":      dlClassQueued,
}

func classifySABStatus(status string) dlClass {
	return sabSlotClasses[status]
}

func classifySAB(slots []sabnzbd.Slot) (active, queued int) {
	dlOrFetch := 0
	for _, s := range slots {
		switch classifySABStatus(s.Status) {
		case dlClassActive:
			dlOrFetch++
		case dlClassQueued:
			queued++
		case dlClassNone:
		}
	}
	if dlOrFetch > 0 {
		active = 1
		queued += dlOrFetch - 1
	}
	return active, queued
}

func (g *Goenvoy) sabnzbd(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl, err := sabnzbd.New(c.BaseURL, secret, sabnzbd.WithTimeout(g.timeout), sabnzbd.WithUserAgent(g.userAgent))
	if err != nil {
		return observation{}, fmt.Errorf("sabnzbd client: %w", err)
	}
	version, err := cl.GetVersion(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("sabnzbd version: %w", err)
	}
	q, err := cl.GetQueue(ctx)
	if err != nil {
		return observation{}, fmt.Errorf("sabnzbd queue: %w", err)
	}
	obs := observation{Version: version, Paused: q.Paused || q.PausedAll, DownloadRate: ParseSABSpeed(q.Speed)}
	obs.Active, obs.Queued = classifySAB(q.Slots)
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

// NZBGet v26.3 group status classification.
// Source: nzbgetcom/nzbget@v26.3 (commit 674787848dddabf9047b8fc1a75cf92fe6e41c55)
// - daemon/remote/XmlRpc.cpp:2326-2365 (ListGroupsXmlCommand::DetectStatus)
// - daemon/queue/DownloadInfo.h:189,243,516-517,702,796-815 (ActiveDownloads, PostInfo::EStage)
//
// | Status String          | Classification | Reason (file:line)                                         |
// | :--------------------- | :------------- | :--------------------------------------------------------- |
// | "DOWNLOADING"          | active         | ArticleDownloader active on NZB articles (XmlRpc.cpp:2352) |
// | "FETCHING"             | queued         | UrlDownloader fetching .nzb URL (XmlRpc.cpp:2354)         |
// | "QUEUED"               | queued         | Waiting in queue without active downloaders (XmlRpc:2360)  |
// | "PAUSED"               | none           | Group paused (XmlRpc.cpp:2356)                             |
// | "QS_QUEUED"            | none           | Queue script queued (XmlRpc.cpp:2344)                      |
// | "QS_EXECUTING"         | none           | Queue script running (XmlRpc.cpp:2344)                     |
// | "PP_QUEUED"            | none           | Post-processing queued (XmlRpc.cpp:2328)                   |
// | "LOADING_PARS"         | none           | Post-processing loading par2 (XmlRpc.cpp:2329)             |
// | "VERIFYING_SOURCES"    | none           | Post-processing verifying sources (XmlRpc.cpp:2330)        |
// | "REPAIRING"            | none           | Post-processing par2 repair (XmlRpc.cpp:2331)             |
// | "VERIFYING_REPAIRED"   | none           | Post-processing verifying repaired (XmlRpc.cpp:2332)       |
// | "RENAMING"             | none           | Post-processing par/rar rename (XmlRpc.cpp:2333)           |
// | "UNPACKING"            | none           | Post-processing archive unpack (XmlRpc.cpp:2334)           |
// | "MOVING"               | none           | Post-processing cleanup/move (XmlRpc.cpp:2335)             |
// | "POST_UNPACK_RENAMING" | none           | Post-processing post-unpack rename (XmlRpc.cpp:2336)       |
// | "EXECUTING_SCRIPT"     | none           | Post-processing extension script (XmlRpc.cpp:2337)         |
// | "PP_FINISHED"          | none           | Post-processing finished (XmlRpc.cpp:2338)                 |.
var nzbgetStateClasses = map[string]dlClass{
	"DOWNLOADING": dlClassActive,
	"FETCHING":    dlClassQueued,
	"QUEUED":      dlClassQueued,
}

func classifyNZBGet(status string) dlClass {
	return nzbgetStateClasses[status]
}

func (g *Goenvoy) nzbget(ctx context.Context, c domain.DownloadClient, secret string) (observation, error) {
	cl, err := nzbget.New(c.BaseURL, c.Username, secret, nzbget.WithTimeout(g.timeout), nzbget.WithUserAgent(g.userAgent))
	if err != nil {
		return observation{}, fmt.Errorf("nzbget client: %w", err)
	}
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
		switch classifyNZBGet(grp.Status) {
		case dlClassActive:
			obs.Active++
		case dlClassQueued:
			obs.Queued++
		case dlClassNone:
		}
	}
	return obs, nil
}
