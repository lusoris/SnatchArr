// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { getClock } from '@sveltesentio/core';

/** How a stamina gauge reads: fresh below 75 %, getting tired below 100 %, spent after. */
export type StaminaLevel = 'fresh' | 'tired' | 'spent';

const TIRED_RATIO = 0.75;

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

/** Formats an ISO timestamp relative to the current time without touching Date.now(). */
export function formatRelativeTime(
	iso: string | undefined,
	locale: string,
	now: Date = getClock().now()
): string {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	const diffSec = Math.round((now.getTime() - d.getTime()) / 1000);
	const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
	if (Math.abs(diffSec) < 5) return rtf.format(0, 'second');
	if (Math.abs(diffSec) < 60) return rtf.format(-diffSec, 'second');
	if (Math.abs(diffSec) < 3600) return rtf.format(-Math.round(diffSec / 60), 'minute');
	if (Math.abs(diffSec) < 86400) return rtf.format(-Math.round(diffSec / 3600), 'hour');
	if (Math.abs(diffSec) < 2592000) return rtf.format(-Math.round(diffSec / 86400), 'day');
	return rtf.format(-Math.round(diffSec / 2592000), 'month');
}
