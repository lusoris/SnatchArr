// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';
import type { StatusKind } from '#lib/components/StatusIcon.svelte';
import { logLevelLabel } from '#lib/logs.js';
import { m } from '#lib/paraglide/messages.js';
import { countExhausted, countHeadroom } from '#lib/prowlarr.js';
import { staminaLevel, type StaminaLevel } from '#lib/stamina.js';

export type Instance = components['schemas']['Instance'];
export type CapStatus = components['schemas']['CapStatus'];
export type ProwlarrStatus = components['schemas']['ProwlarrStatus'];
export type Run = components['schemas']['Run'];
export type Schedule = components['schemas']['Schedule'];

export interface SearchesKpi {
	used: number;
	cap: number;
	status: StatusKind;
	level: StaminaLevel;
	hasGlobal: boolean;
	resetsAt?: string;
}

export interface FleetKpi {
	total: number;
	healthy: number;
	failing: number;
	disabled: number;
	status: StatusKind;
}

export interface ProwlarrKpi {
	total: number;
	headroom: number;
	exhausted: number;
	status: StatusKind;
}

export function instanceStatus(inst: Instance): { status: StatusKind; label: string } {
	if (!inst.enabled) {
		return { status: 'disabled', label: m.instance_disabled() };
	}
	if (inst.last_error) {
		return { status: 'danger', label: `${m.instance_check_failed()}: ${inst.last_error}` };
	}
	if (!inst.last_check_at) {
		return { status: 'idle', label: m.instance_never_tested() };
	}
	return { status: 'ok', label: m.instance_last_check() };
}

export function kpiSearches(caps: CapStatus[] | undefined): SearchesKpi {
	const globalCap = caps?.find((c) => c.scope === 'global');
	if (globalCap) {
		const level = staminaLevel(globalCap.used, globalCap.cap);
		const status: StatusKind = level === 'spent' ? 'danger' : level === 'tired' ? 'warn' : 'ok';
		return {
			used: globalCap.used,
			cap: globalCap.cap,
			status,
			level,
			hasGlobal: true,
			resetsAt: globalCap.resets_at
		};
	}
	const totalUsed = (caps ?? []).reduce((acc, c) => acc + c.used, 0);
	return {
		used: totalUsed,
		cap: 0,
		status: 'ok',
		level: 'fresh',
		hasGlobal: false
	};
}

export function kpiFleet(instances: Instance[] | undefined): FleetKpi {
	const list = instances ?? [];
	const total = list.length;
	if (total === 0) {
		return { total: 0, healthy: 0, failing: 0, disabled: 0, status: 'idle' };
	}
	let healthy = 0;
	let failing = 0;
	let disabled = 0;
	for (const inst of list) {
		if (!inst.enabled) {
			disabled += 1;
		} else if (inst.last_error) {
			failing += 1;
		} else {
			healthy += 1;
		}
	}
	const status: StatusKind = failing > 0 ? (healthy === 0 ? 'danger' : 'warn') : 'ok';
	return { total, healthy, failing, disabled, status };
}

export function kpiProwlarr(links: ProwlarrStatus[] | undefined): ProwlarrKpi {
	const list = links ?? [];
	if (list.length === 0) {
		return { total: 0, headroom: 0, exhausted: 0, status: 'idle' };
	}
	let total = 0;
	let headroom = 0;
	let exhausted = 0;
	for (const link of list) {
		total += link.indexers.length;
		headroom += countHeadroom(link.indexers);
		exhausted += countExhausted(link.indexers);
	}
	const status: StatusKind = exhausted > 0 ? (headroom === 0 ? 'danger' : 'warn') : 'ok';
	return { total, headroom, exhausted, status };
}

export function eventStatus(level: string | undefined): StatusKind {
	switch (level) {
		case 'error':
			return 'danger';
		case 'warn':
			return 'warn';
		case 'info':
			return 'info';
		case 'debug':
			return 'idle';
		default:
			return 'info';
	}
}

export function eventLevelLabel(level: string | undefined): string {
	if (!level) return '';
	switch (level) {
		case 'debug':
		case 'info':
		case 'warn':
		case 'error':
			return logLevelLabel(level);
		default:
			return level;
	}
}

export function lastRunForInstance(runs: Run[] | undefined, instanceId: string): Run | undefined {
	return runs?.find((r) => r.instance_id === instanceId);
}

export function nextScheduleForInstance(
	schedules: Schedule[] | undefined,
	instanceId: string
): Schedule | undefined {
	if (!schedules || schedules.length === 0) return undefined;
	return (
		schedules.find((s) => s.instance_id === instanceId && s.enabled) ??
		schedules.find((s) => !s.instance_id && s.enabled)
	);
}

export function formatScheduleWindow(schedule: Schedule | undefined): string {
	if (!schedule) return '—';
	return `${schedule.start}–${schedule.end} ${schedule.tz}`;
}

export function activePauseText(s: Schedule, instances?: Instance[]): string {
	if (s.action !== 'pause' || !s.active) return '';
	const match = s.instance_id ? instances?.find((i) => i.id === s.instance_id) : undefined;
	const target = match?.name ?? m.schedule_all_instances();
	return m.schedule_active_pause({ target });
}
