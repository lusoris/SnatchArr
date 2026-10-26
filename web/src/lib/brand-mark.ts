// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/** Brand mark choice variants (ADR-0042 section 1). */
export type BrandMarkKind = 'vulva' | 'penis' | 'hand';

/** Safe-for-work mode setting variants (ADR-0042 section 4). */
export type SfwMode = 'auto' | 'on' | 'off';

/** Default brand mark choice (vulva, ADR-0017 and ADR-0042 section 7). */
export const DEFAULT_BRAND_MARK: BrandMarkKind = 'vulva';

/** Default SFW mode (auto, coupled to adult module and user permission, ADR-0042 section 4.2). */
export const DEFAULT_SFW_MODE: SfwMode = 'auto';

/**
 * Resolves the effective brand mark from preferences and system capabilities.
 *
 * Rules:
 * - 'on': always resolves to 'hand' (forced SFW).
 * - 'off': resolves to the preferred brand mark unconditionally.
 * - 'auto': resolves to the preferred brand mark only when both the adult module
 *   is enabled and the user is permitted adult access; otherwise fails closed to 'hand'.
 */
export function resolveBrandMark(
	brandMark: BrandMarkKind,
	sfwMode: SfwMode,
	adultModuleEnabled: boolean,
	userAdultAllowed: boolean
): BrandMarkKind {
	if (sfwMode === 'on') {
		return 'hand';
	}
	if (sfwMode === 'off') {
		return brandMark;
	}
	return adultModuleEnabled && userAdultAllowed ? brandMark : 'hand';
}

/**
 * Mark shown before the session and the module list are known (login, setup, first paint):
 * the defaults resolved with the adult module off, so it fails closed (ADR-0042 section 7).
 */
export const FALLBACK_BRAND_MARK: BrandMarkKind = resolveBrandMark(
	DEFAULT_BRAND_MARK,
	DEFAULT_SFW_MODE,
	false,
	false
);
