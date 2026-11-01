// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/downloadclient/deluge/v2"
	"github.com/golusoris/goenvoy/downloadclient/nzbget/v2"
	"github.com/golusoris/goenvoy/downloadclient/qbit/v2"
	"github.com/golusoris/goenvoy/downloadclient/rtorrent/v2"
	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const probeTimeout = 10 * time.Second

// capability describes one download client's queue priority capability expectation.
type capability struct {
	Method    string `json:"method"`
	Supported bool   `json:"supported"`
	Source    string `json:"source"`
}

// priorityResult holds discovered runtime priority details for a client.
type priorityResult struct {
	Version   string
	Method    string
	Supported bool
}

// wantPriority maps each client kind to its expected priority capability and documentation reference.
var wantPriority = map[domain.DownloadClientKind]capability{
	domain.ClientQBittorrent:  {Method: "torrents/topPrio", Supported: true, Source: "https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)"},
	domain.ClientTransmission: {Method: "queue-move-top", Supported: true, Source: "https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md"},
	domain.ClientDeluge:       {Method: "core.queue_top", Supported: true, Source: "https://deluge.readthedocs.io/en/latest/modules/core/daemon.html"},
	domain.ClientRTorrent:     {Method: "d.priority.set", Supported: true, Source: "https://rtorrent-docs.readthedocs.io/en/latest/cmd-ref.html"},
	domain.ClientSABnzbd:      {Method: "mode=queue&name=priority", Supported: true, Source: "https://sabnzbd.org/wiki/advanced/api"},
	domain.ClientNZBGet:       {Method: "GroupSetPriority", Supported: true, Source: "https://nzbget.com/documentation/api/"},
}

// TestLivePriorityCapabilities discovers whether each running download client exposes priority APIs.
func TestLivePriorityCapabilities(t *testing.T) {
	t.Parallel()
	flipKind := domain.DownloadClientKind(os.Getenv("SNATCHARR_DLCLIENTS_PRIORITY_FLIP"))
	forEachLiveClient(t, func(t *testing.T, c liveClient) {
		t.Helper()
		checkPriorityClient(t, c, flipKind)
	})
}

// checkPriorityClient probes one live download client for priority capabilities and compares against wantPriority.
func checkPriorityClient(t *testing.T, c liveClient, flipKind domain.DownloadClientKind) {
	t.Helper()
	got, err := probeClient(t.Context(), c)
	if err != nil {
		t.Fatalf("probe %s: %v", c.Kind, err)
	}
	t.Logf("%s: version=%s method=%s supported=%t", c.Kind, got.Version, got.Method, got.Supported)
	want, ok := wantPriority[c.Kind]
	if !ok {
		t.Fatalf("no priority capability expectation for %s", c.Kind)
	}
	if c.Kind == flipKind {
		want.Supported = !want.Supported
	}
	if got.Supported != want.Supported || got.Method != want.Method {
		t.Fatalf("priority capability mismatch for %s: got supported=%t method=%q, want supported=%t method=%q",
			c.Kind, got.Supported, got.Method, want.Supported, want.Method)
	}
}

// probeClient dispatches discovery probes to the matching client probe implementation.
func probeClient(ctx context.Context, c liveClient) (priorityResult, error) {
	switch c.Kind {
	case domain.ClientQBittorrent:
		return probeQBittorrent(ctx, c)
	case domain.ClientTransmission:
		return probeTransmission(ctx, c)
	case domain.ClientDeluge:
		return probeDeluge(ctx, c)
	case domain.ClientRTorrent:
		return probeRTorrent(ctx, c)
	case domain.ClientSABnzbd:
		return probeSABnzbd(ctx, c)
	case domain.ClientNZBGet:
		return probeNZBGet(ctx, c)
	default:
		return priorityResult{}, fmt.Errorf("%w: unsupported client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// probeQBittorrent queries qBittorrent WebAPI version and preferences for queueing support.
func probeQBittorrent(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cl, err := qbit.New(c.URL, qbit.WithTimeout(probeTimeout))
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe qbittorrent client: %w", err)
	}
	if c.Username != "" || c.Secret != "" {
		if err = cl.Login(callCtx, c.Username, c.Secret); err != nil {
			return priorityResult{}, fmt.Errorf("probe qbittorrent login: %w", err)
		}
	}
	version, err := cl.WebAPIVersion(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe qbittorrent webapi version: %w", err)
	}
	prefs, err := cl.GetPreferences(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe qbittorrent preferences: %w", err)
	}
	return priorityResult{Version: version, Method: wantPriority[c.Kind].Method, Supported: prefs.QueueingEnabled}, nil
}

// probeTransmission queries Transmission session for RPC version and queue move support.
func probeTransmission(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cl, err := dlclients.TransmissionClient(c.URL, c.Username, c.Secret, probeTimeout, "SnatchArr")
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe transmission client: %w", err)
	}
	session, err := cl.GetSession(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe transmission session: %w", err)
	}
	return priorityResult{Version: session.Version, Method: wantPriority[c.Kind].Method, Supported: session.RPCVersion >= 14}, nil
}

// probeDeluge queries Deluge daemon methods to check queue priority support.
func probeDeluge(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	jar, err := cookiejar.New(nil)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe deluge cookie jar: %w", err)
	}
	httpClient := &http.Client{Jar: jar}
	cl, err := deluge.New(c.URL, deluge.WithHTTPClient(httpClient), deluge.WithTimeout(probeTimeout))
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe deluge client: %w", err)
	}
	if err = cl.Login(callCtx, c.Secret); err != nil {
		return priorityResult{}, fmt.Errorf("probe deluge login: %w", err)
	}
	connected, err := cl.Connected(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe deluge connection: %w", err)
	}
	if !connected {
		return priorityResult{}, dlclients.ErrDelugeNoDaemon
	}
	// goenvoy asks for the version with daemon.info, which Deluge 2 does not serve
	// (golusoris/goenvoy#101); the version is informational, so it stays unknown then.
	version, err := cl.GetVersion(callCtx)
	if err != nil {
		version = ""
	}
	methods, err := delugeMethodList(callCtx, httpClient, c.URL)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe deluge method list: %w", err)
	}
	return priorityResult{Version: version, Method: wantPriority[c.Kind].Method, Supported: slices.Contains(methods, wantPriority[c.Kind].Method)}, nil
}

// delugeMethodList queries daemon.get_method_list because goenvoy deluge/v2 exports no generic JSON-RPC call or method list.
func delugeMethodList(ctx context.Context, client *http.Client, baseURL string) ([]string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(`{"method":"daemon.get_method_list","params":[],"id":1}`))
	if err != nil {
		return nil, fmt.Errorf("deluge create method list request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deluge post method list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deluge method list HTTP %d: %w", resp.StatusCode, domain.ErrInvalid)
	}
	var rpcResp struct {
		Result []string                  `json:"result"`
		Error  *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("deluge decode method list: %w", err)
	}
	if rpcResp.Error != nil && rpcResp.Error.Message != "" {
		return nil, fmt.Errorf("deluge rpc error: %s: %w", rpcResp.Error.Message, domain.ErrInvalid)
	}
	return rpcResp.Result, nil
}

// probeRTorrent queries rTorrent system info and methods for priority support.
func probeRTorrent(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	opts := []rtorrent.Option{rtorrent.WithTimeout(probeTimeout)}
	if c.Username != "" {
		opts = append(opts, rtorrent.WithAuth(c.Username, c.Secret))
	}
	cl, err := rtorrent.New(c.URL, opts...)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe rtorrent client: %w", err)
	}
	info, err := cl.GetSystemInfo(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe rtorrent system info: %w", err)
	}
	methods, err := rtorrentListMethods(callCtx, c.URL, c.Username, c.Secret)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe rtorrent method list: %w", err)
	}
	wantMethod := wantPriority[c.Kind].Method
	supported := slices.Contains(methods, wantMethod)
	return priorityResult{Version: info.ClientVersion, Method: wantMethod, Supported: supported}, nil
}

// rtorrentListMethods queries system.listMethods because goenvoy rtorrent/v2 exports no generic XML-RPC call or method list.
func rtorrentListMethods(ctx context.Context, endpoint, username, secret string) ([]string, error) {
	reqXML := `<?xml version="1.0"?><methodCall><methodName>system.listMethods</methodName><params/></methodCall>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(reqXML))
	if err != nil {
		return nil, fmt.Errorf("rtorrent create request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml")
	if username != "" || secret != "" {
		req.SetBasicAuth(username, secret)
	}
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rtorrent post listMethods: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rtorrent listMethods HTTP %d: %w", resp.StatusCode, domain.ErrInvalid)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rtorrent read listMethods: %w", err)
	}
	var listResp struct {
		Methods []string `xml:"params>param>value>array>data>value>string"`
	}
	if err = xml.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("rtorrent decode listMethods: %w", err)
	}
	if len(listResp.Methods) == 0 {
		return nil, fmt.Errorf("rtorrent method list empty: %w", domain.ErrInvalid)
	}
	return listResp.Methods, nil
}

// probeSABnzbd queries SABnzbd version and authenticated queue read; the priority call itself comes from the API docs (SABnzbd lists no methods).
func probeSABnzbd(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cl, err := sabnzbd.New(c.URL, c.Secret, sabnzbd.WithTimeout(probeTimeout))
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe sabnzbd client: %w", err)
	}
	version, err := cl.GetVersion(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe sabnzbd version: %w", err)
	}
	queue, err := cl.GetQueue(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe sabnzbd queue: %w", err)
	}
	return priorityResult{Version: version, Method: wantPriority[c.Kind].Method, Supported: queue != nil}, nil
}

// probeNZBGet queries NZBGet version and authenticated status read; the priority call itself comes from the API docs (NZBGet lists no methods).
func probeNZBGet(ctx context.Context, c liveClient) (priorityResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cl, err := nzbget.New(c.URL, c.Username, c.Secret, nzbget.WithTimeout(probeTimeout))
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe nzbget client: %w", err)
	}
	version, err := cl.GetVersion(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe nzbget version: %w", err)
	}
	status, err := cl.GetStatus(callCtx)
	if err != nil {
		return priorityResult{}, fmt.Errorf("probe nzbget status: %w", err)
	}
	return priorityResult{Version: version, Method: wantPriority[c.Kind].Method, Supported: status != nil}, nil
}
