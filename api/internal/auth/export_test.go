// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import "context"

// SetPostPasswordUpdateHook sets a hook for fault injection after updating the password.
func (s *Service) SetPostPasswordUpdateHook(fn func(ctx context.Context)) {
	s.testHookPostPasswordUpdate = fn
}
