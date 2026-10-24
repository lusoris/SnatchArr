// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { m } from '#lib/paraglide/messages.js';

export type AppRoute =
	'/' | '/activity' | '/instances' | '/automation' | '/connections' | '/system' | '/account';

export interface NavItem {
	href: AppRoute;
	label: string;
	icon: string;
}

/**
 * Returns top-level navigation items according to ADR-0029 section 1.
 * Exactly 6 primary entries in fixed order: Overview, Activity, Instances, Automation, Connections, System.
 */
export function getNavItems(): NavItem[] {
	return [
		{ href: '/', label: m.nav_overview(), icon: 'layout-dashboard' },
		{ href: '/activity', label: m.nav_activity(), icon: 'activity' },
		{ href: '/instances', label: m.nav_instances(), icon: 'server' },
		{ href: '/automation', label: m.nav_automation(), icon: 'clock' },
		{ href: '/connections', label: m.nav_connections(), icon: 'link' },
		{ href: '/system', label: m.nav_system(), icon: 'settings' }
	];
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
