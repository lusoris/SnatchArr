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
		want    arrclient.Product
	}{
		// Positive.
		{"readarr as recorded", domain.KindReadarr, "Readarr", "0.4.18.2805", linuxserver, arrclient.ProductReadarr},
		{"readarr nightly as recorded", domain.KindReadarr, "Readarr", "0.4.19.2811", linuxserver, arrclient.ProductReadarr},
		{"bookshelf as recorded", domain.KindReadarr, "Readarr", "0.4.21.182", pennydreadful, arrclient.ProductBookshelf},
		{"chaptarr as recorded", domain.KindReadarr, "Chaptarr", "0.9.965.0", "", arrclient.ProductChaptarr},
		{"faustvii as recorded", domain.KindReadarr, "Readarr", "0.10.0.0", faustvii, arrclient.ProductReadarrFaustvii},
		{"readarr from another packager", domain.KindReadarr, "Readarr", "0.4.18.2805", "", arrclient.ProductReadarr},
		{"the packager in another case", domain.KindReadarr, "readarr", "0.4.21.182", "PennyDreadful", arrclient.ProductBookshelf},
		{"the name in another case", domain.KindReadarr, "CHAPTARR", "0.9.965.0", "", arrclient.ProductChaptarr},
		// The packager decides before the version: a fork may still carry a version Readarr built.
		{"bookshelf at a version readarr built", domain.KindReadarr, "Readarr", "0.4.18.2805", pennydreadful, arrclient.ProductBookshelf},
		{"faustvii at a version readarr built", domain.KindReadarr, "Readarr", "0.4.19.0", faustvii, arrclient.ProductReadarrFaustvii},
		// Negative.
		{"a newer readarr from nobody known", domain.KindReadarr, "Readarr", "0.4.21.182", linuxserver, arrclient.ProductUnknown},
		{"another application", domain.KindReadarr, "Lidarr", "0.4.18.2805", linuxserver, arrclient.ProductUnknown},
		{"another application from a known packager", domain.KindReadarr, "Lidarr", "0.4.21.182", pennydreadful, arrclient.ProductUnknown},
		{"no name", domain.KindReadarr, "", "0.4.18.2805", "", arrclient.ProductUnknown},
		{"no version", domain.KindReadarr, "Readarr", "", "", arrclient.ProductUnknown},
		{"a version that is no number", domain.KindReadarr, "Readarr", "nightly", "", arrclient.ProductUnknown},
		{"a version with a letter in it", domain.KindReadarr, "Readarr", "0.4.x", "", arrclient.ProductUnknown},
		{"a negative version", domain.KindReadarr, "Readarr", "0.-4.18", "", arrclient.ProductUnknown},
		{"a kind one product serves", domain.KindLidarr, "Lidarr", "2.8.2", linuxserver, arrclient.ProductNone},
		{"a fork's answer behind another kind", domain.KindLidarr, "Chaptarr", "0.9.965.0", "", arrclient.ProductNone},
		// Boundary: 0.4.19 is the last line the retired Readarr built.
		{"the last readarr line", domain.KindReadarr, "Readarr", "0.4.19", "", arrclient.ProductReadarr},
		{"the last readarr line, any build", domain.KindReadarr, "Readarr", "0.4.19.99999", "", arrclient.ProductReadarr},
		{"one patch above", domain.KindReadarr, "Readarr", "0.4.20.0", "", arrclient.ProductUnknown},
		{"one minor above", domain.KindReadarr, "Readarr", "0.5.0.0", "", arrclient.ProductUnknown},
		{"one major above", domain.KindReadarr, "Readarr", "1.0.0.0", "", arrclient.ProductUnknown},
		{"a lower minor with a higher patch", domain.KindReadarr, "Readarr", "0.3.99.1", "", arrclient.ProductReadarr},
		{"two numbers only", domain.KindReadarr, "Readarr", "0.4", "", arrclient.ProductReadarr},
		{"one number only", domain.KindReadarr, "Readarr", "1", "", arrclient.ProductUnknown},
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
