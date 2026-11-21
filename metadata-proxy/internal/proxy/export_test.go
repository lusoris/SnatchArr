// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import "syscall"

var AllowAllDialControl func(string, string, syscall.RawConn) error = allowAllDialControl
