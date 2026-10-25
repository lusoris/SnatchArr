// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { StatusKind } from '#lib/components/StatusIcon.svelte';
import { m } from '#lib/paraglide/messages.js';
import { formatRelativeTime } from '#lib/stamina.js';

export interface ServiceCounts {
	total: number;
	healthy: number;
	failing: number;
	untested: number;
}

export interface ServiceStatusSummary {
	status: StatusKind;
	counts: ServiceCounts;
	label: string;
	countLine: string;
}

export interface HealthItem {
	reachable?: boolean | null;
	error?: string | null;
	checkedAt?: string | null;
}

export interface ConfigarrStatusPayload {
	linked: boolean;
	config_path?: string | null;
	secrets_path?: string | null;
	watching?: boolean;
	last_import_at?: string | null;
	last_error?: string | null;
}

/** Evaluates overall service health from an array of health observation items. */
export function deriveServiceStatus(items: readonly HealthItem[]): ServiceStatusSummary {
	const total = items.length;
	if (total === 0) {
		return {
			status: 'idle',
			counts: { total: 0, healthy: 0, failing: 0, untested: 0 },
			label: m.connections_health_not_linked(),
			countLine: ''
		};
	}

	let healthy = 0;
	let failing = 0;
	let untested = 0;

	for (const item of items) {
		if (item.error || item.reachable === false) {
			failing++;
		} else if (item.reachable === true || Boolean(item.checkedAt)) {
			healthy++;
		} else {
			untested++;
		}
	}

	const counts: ServiceCounts = { total, healthy, failing, untested };
	return buildServiceSummary(counts);
}

function buildServiceSummary(counts: ServiceCounts): ServiceStatusSummary {
	const { total, healthy, failing } = counts;
	if (failing > 0 && healthy === 0) {
		return {
			status: 'danger',
			counts,
			label: m.connections_health_danger(),
			countLine: m.connections_status_failing({ total, failing })
		};
	}

	if (failing > 0 && healthy > 0) {
		return {
			status: 'warn',
			counts,
			label: m.connections_health_warn(),
			countLine: m.connections_status_failing({ total, failing })
		};
	}

	if (healthy > 0) {
		return {
			status: 'ok',
			counts,
			label: m.connections_health_ok(),
			countLine: m.connections_status_linked({ count: total })
		};
	}

	return {
		status: 'idle',
		counts,
		label: m.instance_never_tested(),
		countLine: m.connections_status_linked({ count: total })
	};
}

/** Evaluates individual link health (used by LinkCard for Seerr and Prowlarr). */
export function deriveLinkStatus(link: {
	last_error?: string | null;
	last_check_at?: string | null;
}): { status: StatusKind; label: string } {
	if (link.last_error) {
		return { status: 'danger', label: link.last_error };
	}
	if (link.last_check_at) {
		return { status: 'ok', label: m.connections_health_ok() };
	}
	return { status: 'idle', label: m.instance_never_tested() };
}

/** Evaluates Configarr singleton sync and import status. */
export function deriveConfigarrStatus(
	st: ConfigarrStatusPayload | undefined | null,
	locale: string
): ServiceStatusSummary {
	if (!st || !st.linked) {
		return {
			status: 'idle',
			counts: { total: 0, healthy: 0, failing: 0, untested: 0 },
			label: m.connections_health_not_linked(),
			countLine: ''
		};
	}

	const hasError = Boolean(st.last_error);
	const hasImport = Boolean(st.last_import_at);
	const counts: ServiceCounts = {
		total: 1,
		healthy: !hasError && hasImport ? 1 : 0,
		failing: hasError ? 1 : 0,
		untested: !hasError && !hasImport ? 1 : 0
	};

	if (hasError) {
		return {
			status: 'danger',
			counts,
			label: m.connections_health_danger(),
			countLine: st.last_import_at
				? m.connections_configarr_linked({ when: formatRelativeTime(st.last_import_at, locale) })
				: m.connections_configarr_linked_no_import()
		};
	}

	if (hasImport && st.last_import_at) {
		return {
			status: 'ok',
			counts,
			label: m.connections_health_ok(),
			countLine: m.connections_configarr_linked({
				when: formatRelativeTime(st.last_import_at, locale)
			})
		};
	}

	return {
		status: 'idle',
		counts,
		label: m.instance_never_tested(),
		countLine: m.connections_configarr_linked_no_import()
	};
}
