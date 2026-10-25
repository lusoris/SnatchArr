// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	buildFilterHref,
	computeGroupStats,
	extractItemCount,
	filterChronologicalLogs,
	groupActivityEvents,
	logStatus,
	matchesActivityFilters,
	resolveScheduleState,
	shouldGroup
} from './activity.js';
import type { LiveFrame } from './live.js';

describe('activity grouping', () => {
	const baseTs = '2026-10-25T10:00:00Z';
	const ts59s = '2026-10-25T10:00:59Z';
	const ts60s = '2026-10-25T10:01:00Z';
	const ts61s = '2026-10-25T10:01:01Z';

	const inst1 = '00000000-0000-0000-0000-000000000001';
	const inst2 = '00000000-0000-0000-0000-000000000002';
	const run1 = '11111111-1111-1111-1111-111111111111';
	const run2 = '22222222-2222-2222-2222-222222222222';

	it('positive: groups consecutive events with identical run_id', () => {
		const events: LiveFrame[] = [
			{
				id: 1,
				run_id: run1,
				instance_id: inst1,
				event: 'run_started',
				type: 'run_started',
				ts: baseTs
			},
			{
				id: 2,
				run_id: run1,
				instance_id: inst1,
				event: 'search_dispatched',
				type: 'search_dispatched',
				detail: '5 items',
				ts: ts59s
			},
			{
				id: 3,
				run_id: run1,
				instance_id: inst1,
				event: 'run_finished',
				type: 'run_finished',
				ts: ts61s
			}
		];

		const groups = groupActivityEvents(events);
		expect(groups).toHaveLength(1);
		expect(groups[0].isGroup).toBe(true);
		expect(groups[0].run_id).toBe(run1);
		expect(groups[0].events).toHaveLength(3);
		expect(groups[0].dispatches).toBe(1);
		expect(groups[0].itemCount).toBe(5);
		expect(groups[0].status).toBe('ok');
	});

	it('negative: does not group consecutive events with different run_ids', () => {
		const events: LiveFrame[] = [
			{ id: 1, run_id: run1, instance_id: inst1, event: 'run_started', ts: baseTs },
			{ id: 2, run_id: run2, instance_id: inst1, event: 'run_started', ts: ts59s }
		];

		const groups = groupActivityEvents(events);
		expect(groups).toHaveLength(2);
		expect(groups[0].run_id).toBe(run1);
		expect(groups[1].run_id).toBe(run2);
	});

	it('boundary: missing run_id groups at 59s and 60s, but splits at 61s', () => {
		const e1: LiveFrame = {
			id: 1,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: baseTs
		};
		const e2_59s: LiveFrame = {
			id: 2,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: ts59s
		};
		const e2_60s: LiveFrame = {
			id: 2,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: ts60s
		};
		const e2_61s: LiveFrame = {
			id: 2,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: ts61s
		};

		// 59s -> grouped
		expect(shouldGroup(e1, e2_59s)).toBe(true);
		const groups59 = groupActivityEvents([e1, e2_59s]);
		expect(groups59).toHaveLength(1);
		expect(groups59[0].isGroup).toBe(true);

		// 60s -> grouped (boundary)
		expect(shouldGroup(e1, e2_60s)).toBe(true);
		const groups60 = groupActivityEvents([e1, e2_60s]);
		expect(groups60).toHaveLength(1);

		// 61s -> NOT grouped (boundary violation)
		expect(shouldGroup(e1, e2_61s)).toBe(false);
		const groups61 = groupActivityEvents([e1, e2_61s]);
		expect(groups61).toHaveLength(2);
		expect(groups61[0].isGroup).toBe(false);
		expect(groups61[1].isGroup).toBe(false);
	});

	it('negative: missing run_id with mixed instances does not group', () => {
		const e1: LiveFrame = {
			id: 1,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: baseTs
		};
		const e2: LiveFrame = {
			id: 2,
			instance_id: inst2,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: ts59s
		};

		expect(shouldGroup(e1, e2)).toBe(false);
		const groups = groupActivityEvents([e1, e2]);
		expect(groups).toHaveLength(2);
	});

	it('negative: missing run_id with mixed types does not group', () => {
		const e1: LiveFrame = {
			id: 1,
			instance_id: inst1,
			type: 'candidates_filtered',
			event: 'candidates_filtered',
			ts: baseTs
		};
		const e2: LiveFrame = {
			id: 2,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: ts59s
		};

		expect(shouldGroup(e1, e2)).toBe(false);
		const groups = groupActivityEvents([e1, e2]);
		expect(groups).toHaveLength(2);
	});

	it('single event yields single item group', () => {
		const e1: LiveFrame = {
			id: 1,
			instance_id: inst1,
			type: 'search_dispatched',
			event: 'search_dispatched',
			ts: baseTs
		};
		const groups = groupActivityEvents([e1]);
		expect(groups).toHaveLength(1);
		expect(groups[0].isGroup).toBe(false);
		expect(groups[0].events).toHaveLength(1);
	});

	it('empty array yields empty groups', () => {
		expect(groupActivityEvents([])).toEqual([]);
	});

	it('extractItemCount extracts count from detail string', () => {
		expect(extractItemCount({ event: 'a', detail: 'Found 12 items' })).toBe(12);
		expect(extractItemCount({ event: 'a', detail: '1 item retrieved' })).toBe(1);
		expect(extractItemCount({ event: 'a', detail: 'No digits here' })).toBe(0);
		expect(extractItemCount({ event: 'a' })).toBe(0);
	});

	it('computeGroupStats reflects highest level and running/ok states', () => {
		const evts: LiveFrame[] = [
			{ event: 'run_started', level: 'info' },
			{ event: 'search_dispatched', level: 'warn', detail: '2 items' }
		];
		const stats = computeGroupStats(evts);
		expect(stats.worstLevel).toBe('warn');
		expect(stats.status).toBe('warn');
		expect(stats.itemCount).toBe(2);
		expect(stats.dispatches).toBe(1);
	});
});

describe('matchesActivityFilters', () => {
	const sample: LiveFrame = {
		id: 1,
		instance_id: 'inst-1',
		level: 'info',
		type: 'search_dispatched',
		event: 'search_dispatched',
		title: 'Search Title',
		detail: 'Detail info',
		ts: '2026-10-25T12:00:00Z'
	};

	it('matches all when filters are empty', () => {
		expect(matchesActivityFilters(sample, {})).toBe(true);
	});

	it('filters by instance', () => {
		expect(matchesActivityFilters(sample, { instance: 'inst-1' })).toBe(true);
		expect(matchesActivityFilters(sample, { instance: 'inst-2' })).toBe(false);
	});

	it('filters by minimum level', () => {
		expect(matchesActivityFilters(sample, { level: 'debug' })).toBe(true);
		expect(matchesActivityFilters(sample, { level: 'info' })).toBe(true);
		expect(matchesActivityFilters(sample, { level: 'warn' })).toBe(false);
		expect(matchesActivityFilters(sample, { level: 'error' })).toBe(false);
	});

	it('filters by type', () => {
		expect(matchesActivityFilters(sample, { type: 'search_dispatched' })).toBe(true);
		expect(matchesActivityFilters(sample, { type: 'run_finished' })).toBe(false);
	});

	it('filters by date range', () => {
		expect(matchesActivityFilters(sample, { from: '2026-10-25T11:00:00Z' })).toBe(true);
		expect(matchesActivityFilters(sample, { from: '2026-10-25T13:00:00Z' })).toBe(false);
		expect(matchesActivityFilters(sample, { to: '2026-10-25T13:00:00Z' })).toBe(true);
		expect(matchesActivityFilters(sample, { to: '2026-10-25T11:00:00Z' })).toBe(false);
	});

	it('filters by search q', () => {
		expect(matchesActivityFilters(sample, { q: 'Title' })).toBe(true);
		expect(matchesActivityFilters(sample, { q: 'detail' })).toBe(true);
		expect(matchesActivityFilters(sample, { q: 'nonexistent' })).toBe(false);
	});
});

describe('logStatus', () => {
	it('maps levels to corresponding StatusKind', () => {
		expect(logStatus('error')).toBe('danger');
		expect(logStatus('warn')).toBe('warn');
		expect(logStatus('warning')).toBe('warn');
		expect(logStatus('debug')).toBe('idle');
		expect(logStatus('info')).toBe('info');
		expect(logStatus(undefined)).toBe('info');
		expect(logStatus('unknown')).toBe('info');
	});
});

describe('resolveScheduleState', () => {
	const instId = 'inst-1';

	it('returns paused when pause action is active', () => {
		const schedules = [
			{ instance_id: instId, enabled: true, action: 'pause', active: true, name: 'Night pause' }
		];
		expect(resolveScheduleState(schedules, instId)).toEqual({ kind: 'paused' });
	});

	it('returns window when an active schedule has a name', () => {
		const schedules = [
			{ instance_id: instId, enabled: true, action: 'snatch', active: true, name: 'Prime Time' }
		];
		expect(resolveScheduleState(schedules, instId)).toEqual({ kind: 'window', name: 'Prime Time' });
	});

	it('returns continuous when no schedule matches or is active', () => {
		const schedules = [
			{ instance_id: instId, enabled: false, action: 'pause', active: true, name: 'Disabled' },
			{ instance_id: 'other', enabled: true, action: 'pause', active: true, name: 'Other inst' }
		];
		expect(resolveScheduleState(schedules, instId)).toEqual({ kind: 'continuous' });
	});
});

describe('buildFilterHref', () => {
	it('positive: updates and removes search parameters', () => {
		const url = new URL('http://localhost/activity?source=events&level=info');
		const result = buildFilterHref(url, { level: 'warn', q: 'test', source: null });
		expect(result).toBe('/activity?level=warn&q=test');
	});

	it('positive: uses fixedInstanceId path when provided', () => {
		const url = new URL('http://localhost/instances/inst-123/activity');
		const result = buildFilterHref(url, { level: 'error' }, 'inst-123');
		expect(result).toBe('/instances/inst-123/activity?level=error');
	});

	it('boundary: empty updates returns same path without changing params', () => {
		const url = new URL('http://localhost/activity?foo=bar');
		expect(buildFilterHref(url, {})).toBe('/activity?foo=bar');
	});
});

describe('filterChronologicalLogs', () => {
	const rec1 = {
		seq: 1,
		time: '2026-10-25T10:00:00Z',
		level: 'info' as const,
		source: 'api',
		message: 'First',
		attrs: []
	};
	const rec2 = {
		seq: 2,
		time: '2026-10-25T11:00:00Z',
		level: 'warn' as const,
		source: 'api',
		message: 'Second',
		attrs: []
	};

	it('positive: sorts chronologically and filters by from/to', () => {
		const filter = { level: 'info' as const, q: '', source: '' };
		const result = filterChronologicalLogs([rec1, rec2], [], filter, '2026-10-25T10:30:00Z');
		expect(result).toHaveLength(1);
		expect(result[0].seq).toBe(2);
	});

	it('negative: returns empty when date range excludes all', () => {
		const filter = { level: 'info' as const, q: '', source: '' };
		const result = filterChronologicalLogs([rec1], [], filter, '2026-10-25T12:00:00Z');
		expect(result).toHaveLength(0);
	});
});
