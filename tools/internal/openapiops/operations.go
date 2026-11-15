// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package openapiops contains shared OpenAPI path-item definitions.
package openapiops

// PathItemOperations lists the OpenAPI 3.1 Path Item Object operation keys in
// specification order.
var PathItemOperations = [...]string{
	"get",
	"put",
	"post",
	"delete",
	"options",
	"head",
	"patch",
	"trace",
}
