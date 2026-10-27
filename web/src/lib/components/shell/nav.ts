// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { m } from '#lib/paraglide/messages.js';

export type AppRoute =
	| '/'
	| '/activity'
	| '/instances'
	| '/automation'
	| '/connections'
	| '/adult/sources'
	| '/adult/performers'
	| '/adult/studios'
	| '/system'
	| '/account';

export interface NavItem {
	href: AppRoute;
	label: string;
	icon: string;
	module?: string;
}

export interface ModuledItem {
	module?: string;
}

/**
 * Drops items whose declared module is not present in activeModules (HISS-19).
 * Items without a module declaration are always retained.
 */
export function filterByModule<T extends object>(
	items: readonly T[],
	activeModules: ReadonlySet<string> = new Set()
): T[] {
	return items.filter((item) => {
		const mod = 'module' in item && typeof item.module === 'string' ? item.module : undefined;
		return !mod || activeModules.has(mod);
	});
}

/** Adult module pages; the top bar and the drawer list them in this order. */
export const ADULT_NAV_ITEMS = [
	{ href: '/adult/sources', label: m.nav_adult_sources(), icon: 'heart', module: 'adult' },
	{ href: '/adult/performers', label: m.nav_adult_performers(), icon: 'user', module: 'adult' },
	{ href: '/adult/studios', label: m.nav_adult_studios(), icon: 'film', module: 'adult' }
] as const satisfies readonly NavItem[];

const PRIMARY_NAV_ITEMS: readonly NavItem[] = [
	{ href: '/', label: m.nav_overview(), icon: 'layout-dashboard' },
	{ href: '/activity', label: m.nav_activity(), icon: 'activity' },
	{ href: '/instances', label: m.nav_instances(), icon: 'server' },
	{ href: '/automation', label: m.nav_automation(), icon: 'clock' },
	{ href: '/connections', label: m.nav_connections(), icon: 'link' },
	...ADULT_NAV_ITEMS,
	{ href: '/system', label: m.nav_system(), icon: 'settings' }
];

/**
 * Returns top-level navigation items according to ADR-0029 section 1.
 * Exactly 6 primary entries in fixed order: Overview, Activity, Instances, Automation, Connections, System.
 * Drops items whose declared module is not present in activeModules (fail closed).
 */
export function getNavItems(activeModules: ReadonlySet<string> = new Set()): NavItem[] {
	return filterByModule(PRIMARY_NAV_ITEMS, activeModules);
}

/**
 * Determines whether targetPath is considered active given currentPath.
 * Root '/' is only active on exact match; other routes match prefix boundaries.
 */
export function isRouteActive(currentPath: string, targetPath: string): boolean {
	if (targetPath === '/') {
		return currentPath === '/';
	}
	return currentPath === targetPath || currentPath.startsWith(`${targetPath}/`);
}

/**
 * Known route prefixes mapped to their governing optional module ID (ADR-0025 section 6).
 * The first module-owned route registers here, e.g. ADR-0024 watch-aware UI.
 */
export const MODULE_ROUTES: Readonly<Record<string, string>> = {
	'/adult': 'adult'
};

/**
 * Resolves the controlling module ID for a pathname, if any.
 */
export function moduleForRoute(
	pathname: string,
	routes: Readonly<Record<string, string>> = MODULE_ROUTES
): string | undefined {
	const norm = pathname.endsWith('/') && pathname.length > 1 ? pathname.slice(0, -1) : pathname;
	for (const [prefix, moduleId] of Object.entries(routes)) {
		if (norm === prefix || norm.startsWith(`${prefix}/`)) {
			return moduleId;
		}
	}
	return undefined;
}

/**
 * Checks whether navigation to pathname is blocked because its module is disabled.
 */
export function isRouteDisabled(
	pathname: string,
	activeModules: ReadonlySet<string>,
	routes: Readonly<Record<string, string>> = MODULE_ROUTES
): { disabled: boolean; moduleId?: string } {
	const moduleId = moduleForRoute(pathname, routes);
	if (!moduleId) {
		return { disabled: false };
	}
	return {
		disabled: !activeModules.has(moduleId),
		moduleId
	};
}
