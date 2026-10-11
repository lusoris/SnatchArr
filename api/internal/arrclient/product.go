// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"strconv"
	"strings"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Product names what answered the probe of an instance kind that several products serve.
// Today that is the kind `readarr` alone: forks of the retired Readarr keep its /api/v1
// layout (ADR-0008), so the kind does not say which of them an instance is.
type Product string

const (
	// ProductNone is the answer for a kind that one product serves.
	ProductNone Product = ""
	// ProductReadarr is a build of the retired Readarr.
	ProductReadarr Product = "readarr"
	// ProductBookshelf is github.com/pennydreadful/bookshelf.
	ProductBookshelf Product = "bookshelf"
	// ProductChaptarr is github.com/Chaptarr/chaptarr.
	ProductChaptarr Product = "chaptarr"
	// ProductReadarrFaustvii is github.com/Faustvii/Readarr.
	ProductReadarrFaustvii Product = "readarr_faustvii"
	// ProductUnknown is an answer that matches none of the products above.
	ProductUnknown Product = "unknown"
)

// versionParts is how many leading numbers of a version order two builds.
const versionParts = 3

// lastReadarr is the last version line the retired Readarr built; its nightly reports 0.4.19.
var lastReadarr = [versionParts]int{0, 4, 19}

// detectProduct tells the products apart that share one instance kind.
func detectProduct(kind domain.AppKind, st *arr.StatusResponse) Product {
	if kind != domain.KindReadarr {
		return ProductNone
	}
	return readarrProduct(st)
}

// readarrProduct reads the product from a system/status answer. Chaptarr reports its own
// name. The other forks report `Readarr`, so their packager and, last, the version decide:
// a Readarr that is newer than the retired project ever was is a fork nobody named here.
func readarrProduct(st *arr.StatusResponse) Product {
	author := strings.ToLower(st.PackageAuthor)
	switch {
	case strings.EqualFold(st.AppName, "Chaptarr"):
		return ProductChaptarr
	case !strings.EqualFold(st.AppName, "Readarr"):
		return ProductUnknown
	case strings.Contains(author, "pennydreadful"):
		return ProductBookshelf
	case strings.Contains(author, "faustvii"):
		return ProductReadarrFaustvii
	case builtByReadarr(st.Version):
		return ProductReadarr
	default:
		return ProductUnknown
	}
}

// builtByReadarr reports whether the retired Readarr built a version, which is one that is
// not above lastReadarr. A missing trailing number counts as zero; anything that is not a
// number makes the version unreadable, and an unreadable version is not Readarr's.
func builtByReadarr(version string) bool {
	parts := strings.SplitN(version, ".", versionParts+1)
	for i, last := range lastReadarr {
		n := 0
		if i < len(parts) {
			parsed, err := strconv.Atoi(parts[i])
			if err != nil || parsed < 0 {
				return false
			}
			n = parsed
		}
		if n != last {
			return n < last
		}
	}
	return true
}
