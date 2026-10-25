// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/**
 * Static mapping of legacy route paths to new task-flow route targets.
 * Kept for one minor release (ADR-0029 section 1); removal tracked in OPEN item.
 */
export const LEGACY_REDIRECT_MAP: Readonly<Record<string, string>> = {
	'/history': '/activity',
	'/live': '/activity?source=live',
	'/logs': '/activity?source=log',
	'/schedules': '/automation?tab=schedules',
	'/download-clients': '/connections/download-clients',
	'/seerr': '/connections/seerr',
	'/prowlarr': '/connections/prowlarr',
	'/configarr': '/connections/configarr',
	'/settings': '/system?tab=general',
	'/about': '/system?tab=about'
};

/**
 * Computes target redirect URL for a legacy route, preserving incoming query parameters.
 * When a parameter appears in both the incoming query and the target definition,
 * the target parameter wins over the incoming duplicate.
 *
 * Kept for one minor release (ADR-0029 section 1); removal tracked in OPEN item.
 */
export function legacyTarget(from: string, url: URL | string): string {
	const normFrom = from.endsWith('/') && from.length > 1 ? from.slice(0, -1) : from;
	const target = LEGACY_REDIRECT_MAP[normFrom] ?? normFrom;

	const dummyBase = 'http://localhost';
	const rawUrl = typeof url === 'string' ? new URL(url, dummyBase) : url;
	const targetUrl = new URL(target, dummyBase);

	// Start with incoming query params
	const merged = new URLSearchParams(rawUrl.searchParams);

	// Target params win over incoming duplicates
	for (const [key, val] of targetUrl.searchParams.entries()) {
		merged.set(key, val);
	}

	const search = merged.toString();
	return search ? `${targetUrl.pathname}?${search}` : targetUrl.pathname;
}
