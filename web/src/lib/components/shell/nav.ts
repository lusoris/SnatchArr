// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { m } from '#lib/paraglide/messages.js';

export type AppRoute =
	| '/'
	| '/account'
	| '/configarr'
	| '/download-clients'
	| '/history'
	| '/instances'
	| '/live'
	| '/logs'
	| '/prowlarr'
	| '/schedules'
	| '/seerr'
	| '/settings';

export interface NavItem {
	href: AppRoute;
	label: string;
	icon: string;
}

export interface NavGroup {
	label: string;
	items: NavItem[];
}

/**
 * Returns navigation groups according to ADR-0015 section 3.
 * System group conditionally includes logs for administrators.
 */
export function getNavGroups(isAdmin: boolean): NavGroup[] {
	return [
		{
			label: m.nav_group_fleet(),
			items: [
				{ href: '/', label: m.nav_dashboard(), icon: 'layout-dashboard' },
				{ href: '/instances', label: m.nav_instances(), icon: 'server' }
			]
		},
		{
			label: m.nav_group_activity(),
			items: [
				{ href: '/history', label: m.nav_history(), icon: 'history' },
				{ href: '/live', label: m.nav_live(), icon: 'radio' }
			]
		},
		{
			label: m.nav_group_automation(),
			items: [{ href: '/schedules', label: m.nav_schedules(), icon: 'clock' }]
		},
		{
			label: m.nav_group_integrations(),
			items: [
				{ href: '/download-clients', label: m.nav_download_clients(), icon: 'cloud-download' },
				{ href: '/seerr', label: m.nav_seerr(), icon: 'film' },
				{ href: '/prowlarr', label: m.nav_prowlarr(), icon: 'search' },
				{ href: '/configarr', label: m.nav_configarr(), icon: 'file-code' }
			]
		},
		{
			label: m.nav_group_system(),
			items: [
				{ href: '/settings', label: m.nav_settings(), icon: 'settings' },
				...(isAdmin ? [{ href: '/logs' as const, label: m.nav_logs(), icon: 'terminal' }] : [])
			]
		}
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
