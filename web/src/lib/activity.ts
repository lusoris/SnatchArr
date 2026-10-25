// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import type { LiveFrame } from './live.js';
import { mergeLogs, type LogFilter, type LogRecord } from './logs.js';

export type ApiEvent = components['schemas']['Event'];
export type StatusKind = 'ok' | 'warn' | 'danger' | 'info' | 'running' | 'idle' | 'disabled';

export interface ActivityFilter {
	instance?: string;
	level?: string;
	type?: string;
	from?: string;
	to?: string;
	q?: string;
}

export interface ActivityGroup {
	id: string;
	isGroup: boolean;
	run_id?: string;
	instance_id?: string;
	type?: string;
	events: LiveFrame[];
	itemCount: number;
	dispatches: number;
	level: string;
	status: StatusKind;
	ts?: string;
	title?: string;
	detail?: string;
}

const LEVEL_RANKS: Record<string, number> = {
	debug: 0,
	info: 1,
	warn: 2,
	warning: 2,
	error: 3
};

export function levelRank(level?: string): number {
	if (!level) return 1;
	return LEVEL_RANKS[level.toLowerCase()] ?? 1;
}

export function parseTime(ts?: string): number {
	if (!ts) return 0;
	const parsed = Date.parse(ts);
	return Number.isNaN(parsed) ? 0 : parsed;
}

export function extractItemCount(event: LiveFrame): number {
	if (!event.detail) return 0;
	const match = event.detail.match(/(\d+)\s+items?/i);
	if (match) {
		const parsed = Number.parseInt(match[1], 10);
		return Number.isNaN(parsed) ? 0 : parsed;
	}
	return 0;
}

export function shouldGroup(prev: LiveFrame, curr: LiveFrame): boolean {
	if (prev.run_id && curr.run_id) {
		return prev.run_id === curr.run_id;
	}
	if (prev.run_id || curr.run_id) {
		return false;
	}
	if (!prev.instance_id || !curr.instance_id || prev.instance_id !== curr.instance_id) {
		return false;
	}
	if (!prev.type || !curr.type || prev.type !== curr.type) {
		return false;
	}
	const tPrev = parseTime(prev.ts);
	const tCurr = parseTime(curr.ts);
	if (tPrev === 0 || tCurr === 0) {
		return false;
	}
	const diffMs = Math.abs(tCurr - tPrev);
	return diffMs <= 60_000;
}

function resolveGroupStatus(events: readonly LiveFrame[], worstLevel: string): StatusKind {
	if (worstLevel === 'error') return 'danger';
	if (worstLevel === 'warn' || worstLevel === 'warning') return 'warn';
	const hasStarted = events.some((e) => e.type === 'run_started' || e.event === 'run_started');
	const hasFinished = events.some(
		(e) =>
			e.type === 'run_finished' ||
			e.event === 'run_finished' ||
			e.type === 'search_completed' ||
			e.event === 'search_completed'
	);
	if (hasStarted && !hasFinished) return 'running';
	if (hasFinished) return 'ok';
	if (worstLevel === 'debug') return 'idle';
	return 'info';
}

export function computeGroupStats(events: readonly LiveFrame[]): {
	dispatches: number;
	itemCount: number;
	worstLevel: string;
	status: StatusKind;
} {
	let dispatches = 0;
	let totalItems = 0;
	let hasItems = false;
	let maxRank = 0;
	let worstLevel = 'info';

	for (const e of events) {
		const lvl = (e.level ?? 'info').toLowerCase();
		const r = levelRank(lvl);
		if (r >= maxRank) {
			maxRank = r;
			worstLevel = lvl;
		}
		if (e.type === 'search_dispatched' || e.event === 'search_dispatched') {
			dispatches++;
		}
		const count = extractItemCount(e);
		if (count > 0) {
			totalItems += count;
			hasItems = true;
		}
	}

	const itemCount = hasItems ? totalItems : Math.max(dispatches, events.length);
	const status = resolveGroupStatus(events, worstLevel);
	return { dispatches, itemCount, worstLevel, status };
}

function buildGroup(events: LiveFrame[], index: number): ActivityGroup {
	const first = events[0];
	const last = events[events.length - 1];
	const stats = computeGroupStats(events);
	const isGroup = events.length > 1;

	const id = first.run_id
		? `run-${first.run_id}-${index}`
		: first.id !== undefined
			? `evt-${first.id}-${index}`
			: `grp-${index}-${first.ts ?? ''}`;

	return {
		id,
		isGroup,
		run_id: first.run_id,
		instance_id: first.instance_id,
		type: first.type ?? first.event,
		events,
		itemCount: stats.itemCount,
		dispatches: stats.dispatches,
		level: stats.worstLevel,
		status: stats.status,
		ts: last.ts ?? first.ts,
		title: first.title,
		detail: first.detail
	};
}

export function groupActivityEvents(events: readonly LiveFrame[]): ActivityGroup[] {
	if (events.length === 0) return [];

	const groups: ActivityGroup[] = [];
	let currentEvents: LiveFrame[] = [events[0]];

	for (let i = 1; i < events.length; i++) {
		const prev = currentEvents[currentEvents.length - 1];
		const curr = events[i];

		if (shouldGroup(prev, curr)) {
			currentEvents.push(curr);
		} else {
			groups.push(buildGroup(currentEvents, groups.length));
			currentEvents = [curr];
		}
	}

	if (currentEvents.length > 0) {
		groups.push(buildGroup(currentEvents, groups.length));
	}

	return groups;
}

function matchesSearch(frame: LiveFrame, q: string): boolean {
	const term = q.toLowerCase();
	if (frame.title?.toLowerCase().includes(term)) return true;
	if (frame.detail?.toLowerCase().includes(term)) return true;
	if (frame.type?.toLowerCase().includes(term)) return true;
	if (frame.event?.toLowerCase().includes(term)) return true;
	if (frame.run_id?.toLowerCase().includes(term)) return true;
	return false;
}

export function matchesActivityFilters(event: LiveFrame, filters: ActivityFilter): boolean {
	if (filters.instance && event.instance_id !== filters.instance) {
		return false;
	}
	if (filters.level && levelRank(event.level) < levelRank(filters.level)) {
		return false;
	}
	if (filters.type && event.type !== filters.type && event.event !== filters.type) {
		return false;
	}
	if (filters.from) {
		const tEvent = parseTime(event.ts);
		const tFrom = parseTime(filters.from);
		if (tFrom > 0 && tEvent < tFrom) return false;
	}
	if (filters.to) {
		const tEvent = parseTime(event.ts);
		const tTo = parseTime(filters.to);
		if (tTo > 0 && tEvent > tTo) return false;
	}
	if (filters.q && !matchesSearch(event, filters.q)) {
		return false;
	}
	return true;
}

export function logStatus(level?: string): StatusKind {
	if (!level) return 'info';
	const lvl = level.toLowerCase();
	if (lvl === 'error') return 'danger';
	if (lvl === 'warn' || lvl === 'warning') return 'warn';
	if (lvl === 'debug') return 'idle';
	return 'info';
}

export interface ScheduleItemLike {
	instance_id?: string;
	enabled: boolean;
	action?: string;
	active?: boolean;
	name?: string;
}

export function resolveScheduleState(
	schedules: readonly ScheduleItemLike[],
	instanceId: string
): { kind: 'paused' } | { kind: 'window'; name: string } | { kind: 'continuous' } {
	const instSchedules = schedules.filter(
		(s) => s.enabled && (!s.instance_id || s.instance_id === instanceId)
	);
	const activePause = instSchedules.find((s) => s.action === 'pause' && s.active);
	if (activePause) return { kind: 'paused' };
	const activeWindow = instSchedules.find((s) => s.active);
	if (activeWindow && activeWindow.name) return { kind: 'window', name: activeWindow.name };
	return { kind: 'continuous' };
}

// The client calls its default timers with itself as receiver, which browsers
// refuse, so it never reconnects (golusoris/sveltesentio#424); bound wrappers
// avoid that until a fixed release is out.
export function createTimerOptions(): {
	setTimeoutImpl: typeof setTimeout;
	clearTimeoutImpl: typeof clearTimeout;
} {
	return {
		setTimeoutImpl: ((cb: TimerHandler, ms?: number) =>
			setTimeout(cb, ms)) as unknown as typeof setTimeout,
		clearTimeoutImpl: ((id?: number | NodeJS.Timeout) =>
			clearTimeout(id)) as unknown as typeof clearTimeout
	};
}

export function buildFilterHref(
	currentUrl: URL,
	updates: Record<string, string | null | undefined>,
	fixedInstanceId?: string
): string {
	const newUrl = new URL(currentUrl.href);
	for (const [k, v] of Object.entries(updates)) {
		if (v !== undefined && v !== null && v !== '') {
			newUrl.searchParams.set(k, v);
		} else {
			newUrl.searchParams.delete(k);
		}
	}
	const base = fixedInstanceId ? `/instances/${fixedInstanceId}/activity` : '/activity';
	return `${base}${newUrl.search}`;
}

export function filterChronologicalLogs(
	records: readonly LogRecord[],
	streamed: readonly LogRecord[],
	filter: LogFilter,
	from?: string,
	to?: string
): LogRecord[] {
	return [...mergeLogs(records, streamed, filter, 500)].reverse().filter((r) => {
		if (from && r.time < from) return false;
		if (to && r.time > to) return false;
		return true;
	});
}
