// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// The stub answers the way rTorrent 0.16.25 writes XML-RPC (src/rpc/xmlrpc_tinyxml2.cc,
// print_object_xml and print_xmlrpc_fault): every number is an i8, the 0 and 1 of d.is_open,
// d.is_active and d.complete included, and a fault is a struct of faultCode and faultString.
// The library version differs from the client version only so that a case tells the two apart.
const (
	rtVersion  = "0.16.25"
	rtLibrary  = "0.16.25-library"
	rtUser     = "rt"
	rtPassword = "pw"
	rtFault    = `<fault><value><struct>` +
		`<member><name>faultCode</name><value><i8>-503</i8></value></member>` +
		`<member><name>faultString</name><value><string>Could not find view</string></value></member>` +
		`</struct></value></fault>`
)

// rtTorrent is what the adapter reads of one download.
type rtTorrent struct {
	downRate, upRate       int64
	open, active, complete bool
}

func rtString(s string) string { return "<value><string>" + s + "</string></value>" }

func rtNumber(n int64) string { return fmt.Sprintf("<value><i8>%d</i8></value>", n) }

func rtFlag(set bool) string {
	if set {
		return rtNumber(1)
	}
	return rtNumber(0)
}

// row is one d.multicall2 result in the order of the fields the client asks for: hash, name,
// size, completed bytes, down rate, up rate, up total, ratio, open, active, complete, hash
// checking, base path, directory, label, start time, message.
func (d rtTorrent) row() string {
	fields := []string{
		rtString("0123456789ABCDEF0123456789ABCDEF01234567"), rtString("a download"),
		rtNumber(4_000_000_000), rtNumber(1_000_000_000), rtNumber(d.downRate), rtNumber(d.upRate),
		rtNumber(0), rtNumber(0), rtFlag(d.open), rtFlag(d.active), rtFlag(d.complete), rtFlag(false),
		rtString("/downloads/a download"), rtString("/downloads"), rtString(""), rtNumber(1_760_000_000),
		rtString(""),
	}
	return "<value><array><data>" + strings.Join(fields, "") + "</data></array></value>"
}

// rtServer serves the four system calls and the torrent listing behind HTTP basic
// authentication. A listing of "fault" answers the listing with an XML-RPC fault.
func rtServer(t *testing.T, listing string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, password, ok := r.BasicAuth(); !ok || user != rtUser || password != rtPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var call struct {
			Method string `xml:"methodName"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Errorf("decode the call: %v", err)
		}
		var result string
		switch call.Method {
		case "system.client_version":
			result = "<params><param>" + rtString(rtVersion) + "</param></params>"
		case "system.library_version":
			result = "<params><param>" + rtString(rtLibrary) + "</param></params>"
		case "system.hostname":
			result = "<params><param>" + rtString("seedbox") + "</param></params>"
		case "system.pid":
			result = "<params><param>" + rtNumber(4242) + "</param></params>"
		case "d.multicall2":
			result = listing
		default:
			t.Errorf("unexpected call %q", call.Method)
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, xml.Header+"<methodResponse>"+result+"</methodResponse>")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rtListing(downloads ...rtTorrent) string {
	rows := make([]string, 0, len(downloads))
	for _, d := range downloads {
		rows = append(rows, d.row())
	}
	return "<params><param><value><array><data>" + strings.Join(rows, "") + "</data></array></value></param></params>"
}

func TestRTorrentSnapshot(t *testing.T) {
	t.Parallel()
	downloading := rtTorrent{downRate: 1000, upRate: 10, open: true, active: true}
	seeding := rtTorrent{upRate: 500, open: true, active: true, complete: true}
	stopped := rtTorrent{open: true}
	closed := rtTorrent{}
	// rTorrent starts only an open download, so it reports no such row; the rule still names
	// the open flag.
	activeNotOpen := rtTorrent{downRate: 7, active: true}
	cases := []struct {
		name       string
		downloads  []rtTorrent
		wantActive int
		wantDown   int64
		wantUp     int64
	}{
		// Positive.
		{name: "one downloads, one seeds", downloads: []rtTorrent{downloading, seeding}, wantActive: 1, wantDown: 1000, wantUp: 510},
		{name: "two download", downloads: []rtTorrent{downloading, downloading}, wantActive: 2, wantDown: 2000, wantUp: 20},
		// Negative: none of these transfers a download.
		{name: "open, active and complete", downloads: []rtTorrent{seeding}, wantUp: 500},
		{name: "open and not active", downloads: []rtTorrent{stopped}},
		{name: "closed", downloads: []rtTorrent{closed}},
		{name: "active and not open", downloads: []rtTorrent{activeNotOpen}, wantDown: 7},
		// Boundary.
		{name: "no download", downloads: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := rtServer(t, rtListing(tc.downloads...))
			s := observe(t, domain.ClientRTorrent, srv.URL, rtUser, rtPassword)
			if !s.Reachable || s.Version != rtVersion {
				t.Fatalf("got %+v, want a reachable client of version %s", s, rtVersion)
			}
			if s.Active != tc.wantActive || s.DownloadRate != tc.wantDown || s.UploadRate != tc.wantUp {
				t.Errorf("active = %d, down = %d, up = %d, want %d, %d, %d", s.Active, s.DownloadRate, s.UploadRate, tc.wantActive, tc.wantDown, tc.wantUp)
			}
			// The adapter reads no waiting state from rTorrent: queued stays 0 by intent
			// (docs/download-clients.md).
			if s.Queued != 0 || s.Paused {
				t.Errorf("queued = %d, paused = %v, want none", s.Queued, s.Paused)
			}
		})
	}
}

func TestRTorrentSnapshot_Refused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		listing    string
		password   string
		wantPrefix string
		wantText   string
	}{
		{name: "a fault on the listing", listing: rtFault, password: rtPassword, wantPrefix: "rtorrent torrents: ", wantText: "Could not find view"},
		{name: "a wrong password", listing: rtListing(), password: "wrong", wantPrefix: "rtorrent system info: ", wantText: "HTTP 401"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := rtServer(t, tc.listing)
			s := observe(t, domain.ClientRTorrent, srv.URL, rtUser, tc.password)
			if s.Reachable || !strings.HasPrefix(s.Error, tc.wantPrefix) || !strings.Contains(s.Error, tc.wantText) {
				t.Fatalf("got %+v, want an unreachable snapshot whose error starts with %q and names %q", s, tc.wantPrefix, tc.wantText)
			}
		})
	}
}
