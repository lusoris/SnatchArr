// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/** How a stamina gauge reads: fresh below 75 %, getting tired below 100 %, spent after. */
export type StaminaLevel = 'fresh' | 'tired' | 'spent';

export const TIRED_RATIO = 0.75;

export function staminaLevel(used: number, cap: number): StaminaLevel {
	if (cap <= 0) return 'spent';
	const ratio = used / cap;
	if (ratio >= 1) return 'spent';
	if (ratio >= TIRED_RATIO) return 'tired';
	return 'fresh';
}

/** Percentage for the gauge width, clamped to 0..100. */
export function staminaPercent(used: number, cap: number): number {
	if (cap <= 0) return 100;
	return Math.max(0, Math.min(100, Math.round((used / cap) * 100)));
}

/** Formats an ISO timestamp for the current locale without touching Date.now(). */
export function formatWhen(iso: string | undefined, locale: string): string {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(d);
}
