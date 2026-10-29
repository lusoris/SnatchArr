// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// The packagers as the images report them.
const (
	linuxserver   = "[linuxserver.io](https://www.linuxserver.io/)"
	pennydreadful = "pennydreadful"
	faustvii      = "[Faustvii](https://github.com/Faustvii)"
	rresurrected  = "[ricetim](https://github.com/ricetim)"
)

// TestProbe_Product probes a fake that answers system/status the way each product does.
// The five "as recorded" rows carry the name, version and packager that a container of the
// product answered with on 2026-10-11.
func TestProbe_Product(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    domain.AppKind
		app     string
		version string
		author  string
		want    domain.Product
	}{
		// Positive.
		{"readarr as recorded", domain.KindReadarr, "Readarr", "0.4.18.2805", linuxserver, domain.ProductReadarr},
		{"readarr nightly as recorded", domain.KindReadarr, "Readarr", "0.4.19.2811", linuxserver, domain.ProductReadarr},
		{"bookshelf as recorded", domain.KindReadarr, "Readarr", "0.4.21.182", pennydreadful, domain.ProductBookshelf},
		{"chaptarr as recorded", domain.KindReadarr, "Chaptarr", "0.9.965.0", "", domain.ProductChaptarr},
		{"faustvii as recorded", domain.KindReadarr, "Readarr", "0.10.0.0", faustvii, domain.ProductReadarrFaustvii},
		{"rresurrected as its image builds it", domain.KindReadarr, "Readarr", "11.3.1.0", rresurrected, domain.ProductReadarrRresurrected},
		{"rresurrected packager in another case", domain.KindReadarr, "Readarr", "11.3.1.0", "[RiceTim](https://github.com/RiceTim)", domain.ProductReadarrRresurrected},
		{"readarr from another packager", domain.KindReadarr, "Readarr", "0.4.18.2805", "", domain.ProductReadarr},
		{"the packager in another case", domain.KindReadarr, "readarr", "0.4.21.182", "PennyDreadful", domain.ProductBookshelf},
		{"the name in another case", domain.KindReadarr, "CHAPTARR", "0.9.965.0", "", domain.ProductChaptarr},
		// The packager decides before the version: a fork may still carry a version Readarr built.
		{"bookshelf at a version readarr built", domain.KindReadarr, "Readarr", "0.4.18.2805", pennydreadful, domain.ProductBookshelf},
		{"faustvii at a version readarr built", domain.KindReadarr, "Readarr", "0.4.19.0", faustvii, domain.ProductReadarrFaustvii},
		{"rresurrected at a version readarr built", domain.KindReadarr, "Readarr", "0.4.18.2805", rresurrected, domain.ProductReadarrRresurrected},
		// A newer Readarr without a packager is a fork that cannot be told apart; the
		// readarr-rresurrected image ships its package_info where Servarr does not read it.
		{"a newer readarr without a packager", domain.KindReadarr, "Readarr", "11.3.1.0", "", domain.ProductReadarrFork},
		{"a newer readarr with a blank packager", domain.KindReadarr, "Readarr", "0.4.21.182", "  ", domain.ProductReadarrFork},
		// Negative.
		{"a newer readarr from nobody known", domain.KindReadarr, "Readarr", "0.4.21.182", linuxserver, domain.ProductUnknown},
		{"another application", domain.KindReadarr, "Lidarr", "0.4.18.2805", linuxserver, domain.ProductUnknown},
		{"a packager whose name starts like rresurrected's", domain.KindReadarr, "Readarr", "11.3.1.0", "[ricetimber](https://github.com/ricetimber)", domain.ProductUnknown},
		{"rresurrected's packager on another application", domain.KindReadarr, "Lidarr", "11.3.1.0", rresurrected, domain.ProductUnknown},
		{"another application from a known packager", domain.KindReadarr, "Lidarr", "0.4.21.182", pennydreadful, domain.ProductUnknown},
		{"no name", domain.KindReadarr, "", "0.4.18.2805", "", domain.ProductUnknown},
		{"no version", domain.KindReadarr, "Readarr", "", "", domain.ProductUnknown},
		{"an unreadable version without a packager", domain.KindReadarr, "Readarr", "11.x", "", domain.ProductUnknown},
		{"a version that is no number", domain.KindReadarr, "Readarr", "nightly", "", domain.ProductUnknown},
		{"a version with a letter in it", domain.KindReadarr, "Readarr", "0.4.x", "", domain.ProductUnknown},
		{"a negative version", domain.KindReadarr, "Readarr", "0.-4.18", "", domain.ProductUnknown},
		{"a kind one product serves", domain.KindLidarr, "Lidarr", "2.8.2", linuxserver, domain.ProductNone},
		{"a fork's answer behind another kind", domain.KindLidarr, "Chaptarr", "0.9.965.0", "", domain.ProductNone},
		// Boundary: 0.4.19 is the last line the retired Readarr built.
		{"the last readarr line", domain.KindReadarr, "Readarr", "0.4.19", "", domain.ProductReadarr},
		{"the last readarr line, any build", domain.KindReadarr, "Readarr", "0.4.19.99999", "", domain.ProductReadarr},
		{"one patch above", domain.KindReadarr, "Readarr", "0.4.20.0", "", domain.ProductReadarrFork},
		{"one minor above", domain.KindReadarr, "Readarr", "0.5.0.0", "", domain.ProductReadarrFork},
		{"one major above", domain.KindReadarr, "Readarr", "1.0.0.0", "", domain.ProductReadarrFork},
		{"a lower minor with a higher patch", domain.KindReadarr, "Readarr", "0.3.99.1", "", domain.ProductReadarr},
		{"two numbers only", domain.KindReadarr, "Readarr", "0.4", "", domain.ProductReadarr},
		{"one number only", domain.KindReadarr, "Readarr", "1", "", domain.ProductReadarrFork},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := fakeStatus(t, arr.StatusResponse{AppName: tc.app, Version: tc.version, PackageAuthor: tc.author}, "k")
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			res, err := p.Probe(t.Context(), tc.kind, srv.URL, "k")
			if err != nil {
				t.Fatalf("Probe() error = %v", err)
			}
			if res.Product != tc.want {
				t.Errorf("Probe() product = %q, want %q", res.Product, tc.want)
			}
			if res.AppName != tc.app || res.Version != tc.version {
				t.Errorf("Probe() = %+v, want the name %q and the version %q unchanged", res, tc.app, tc.version)
			}
		})
	}
}
