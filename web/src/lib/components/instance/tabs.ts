// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { m } from '#lib/paraglide/messages.js';

export type InstanceTabKey = 'overview' | 'policy' | 'queue' | 'activity' | 'memory';

export interface InstanceTab {
	key: InstanceTabKey;
	href: string;
	label: string;
}

/** Returns the 5 instance subtabs defined by ADR-0015 section 4. */
export function getInstanceTabs(instanceId: string): InstanceTab[] {
	const base = `/instances/${instanceId}`;
	return [
		{ key: 'overview', href: base, label: m.instance_tab_overview() },
		{ key: 'policy', href: `${base}/policy`, label: m.instance_tab_policy() },
		{ key: 'queue', href: `${base}/queue`, label: m.instance_tab_queue() },
		{ key: 'activity', href: `${base}/activity`, label: m.instance_tab_activity() },
		{ key: 'memory', href: `${base}/memory`, label: m.instance_tab_memory() }
	];
}

/**
 * Determines whether an instance tab is considered active.
 * Overview matches exact path only; other subtabs match prefix boundaries.
 */
export function isTabActive(currentPath: string, tabHref: string, isOverview = false): boolean {
	if (!currentPath || !tabHref) return false;
	const normCurrent =
		currentPath.endsWith('/') && currentPath.length > 1 ? currentPath.slice(0, -1) : currentPath;
	const normTab = tabHref.endsWith('/') && tabHref.length > 1 ? tabHref.slice(0, -1) : tabHref;

	if (isOverview) {
		return normCurrent === normTab;
	}
	return normCurrent === normTab || normCurrent.startsWith(`${normTab}/`);
}
