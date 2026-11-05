//go:build semgrep_fixture

// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package fixture

import (
	"io"
	"net/http"
)

func responseBodies(resp *http.Response, w http.ResponseWriter) {
	// ruleid: no-unbounded-http-body-read
	_, _ = io.ReadAll(resp.Body)
	// ok: no-unbounded-http-body-read
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1024+1))
	// ok: no-unbounded-http-body-read
	_, _ = io.ReadAll(http.MaxBytesReader(w, resp.Body, 1024))
}
