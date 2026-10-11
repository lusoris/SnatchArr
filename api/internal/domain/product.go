// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

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
