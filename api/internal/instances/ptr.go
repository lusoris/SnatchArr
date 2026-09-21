// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package instances

import "time"

func timePtr(t time.Time) *time.Time { return &t }
