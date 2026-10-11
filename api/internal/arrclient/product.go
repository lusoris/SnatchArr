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

// versionParts is how many leading numbers of a version order two builds.
const versionParts = 3

// lastReadarr is the last version line the retired Readarr built; its nightly reports 0.4.19.
var lastReadarr = [versionParts]int{0, 4, 19}

// detectProduct tells the products apart that share one instance kind.
func detectProduct(kind domain.AppKind, st *arr.StatusResponse) domain.Product {
	if kind != domain.KindReadarr {
		return domain.ProductNone
	}
	return readarrProduct(st)
}

// readarrProduct reads the product from a system/status answer. Chaptarr reports its own
// name. The other forks report `Readarr`, so their packager and, last, the version decide:
// a Readarr that is newer than the retired project ever was is a fork nobody named here.
func readarrProduct(st *arr.StatusResponse) domain.Product {
	author := strings.ToLower(st.PackageAuthor)
	switch {
	case strings.EqualFold(st.AppName, "Chaptarr"):
		return domain.ProductChaptarr
	case !strings.EqualFold(st.AppName, "Readarr"):
		return domain.ProductUnknown
	case strings.Contains(author, "pennydreadful"):
		return domain.ProductBookshelf
	case strings.Contains(author, "faustvii"):
		return domain.ProductReadarrFaustvii
	case builtByReadarr(st.Version):
		return domain.ProductReadarr
	default:
		return domain.ProductUnknown
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
