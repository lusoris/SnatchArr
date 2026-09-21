// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package buildinfo carries the ldflags-injected release metadata into the fx graph.
package buildinfo

// Info describes the running build.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// Name is the product name reported by the API.
const Name = "SnatchArr"
