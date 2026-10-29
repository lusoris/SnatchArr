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

// rresurrectedPackager is how the readarr-rresurrected image names its packager, a link to
// the maintainer's profile; the whole link keeps a similar name from matching.
const rresurrectedPackager = "(https://github.com/ricetim)"

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
// Without a packager such a build is shown as an unidentified fork; a packager that is not
// known here leaves it unknown.
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
	case strings.Contains(author, rresurrectedPackager):
		return domain.ProductReadarrRresurrected
	}
	switch cmp, ok := compareReadarr(st.Version); {
	case !ok:
		return domain.ProductUnknown
	case cmp <= 0:
		return domain.ProductReadarr
	case strings.TrimSpace(author) == "":
		return domain.ProductReadarrFork
	default:
		return domain.ProductUnknown
	}
}

// compareReadarr orders a version against lastReadarr: below zero when the retired Readarr
// built an older line, zero for its last line and above zero for a newer one. A missing
// trailing number counts as zero; a leading part that is not a number makes the version
// unreadable, and ok is false then.
func compareReadarr(version string) (cmp int, ok bool) {
	parts := strings.SplitN(version, ".", versionParts+1)
	var nums [versionParts]int
	for i := range nums {
		if i >= len(parts) {
			break
		}
		parsed, err := strconv.Atoi(parts[i])
		if err != nil || parsed < 0 {
			return 0, false
		}
		nums[i] = parsed
	}
	for i, last := range lastReadarr {
		if nums[i] != last {
			return nums[i] - last, true
		}
	}
	return 0, true
}
