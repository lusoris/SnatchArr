// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

// The zone database is compiled into the test binary: without it the schedule tests skipped
// and passed on a host without /usr/share/zoneinfo.
import _ "time/tzdata"
