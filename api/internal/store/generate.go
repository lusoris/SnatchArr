// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

// sqlc at the tools/go/go.mod pin rewrites internal/store/sqlcgen from the queries and the
// migrations (api/sqlc.yaml), so the "generated code is committed" check of make api-verify and
// CI finds a query or migration changed without its generated code (#178). go -C runs it in the
// tools module; the config path is relative to that directory. gennosec then puts back the
// #nosec annotations sqlc drops; go generate reaches this package after internal/httpapi.
//go:generate go -C ../../../tools/go run github.com/sqlc-dev/sqlc/cmd/sqlc generate -f ../../api/sqlc.yaml
//go:generate go run github.com/lusoris/SnatchArr/tools/gennosec apply
