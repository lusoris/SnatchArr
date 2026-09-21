// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package httpapi implements the OpenAPI contract (api/openapi/openapi.yaml) on top of
// the ogen-generated server in internal/oas. Every error is rendered as RFC 9457.
package httpapi

//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.20.2 --target ../build/oas --package oas --clean ../../openapi/openapi.yaml
