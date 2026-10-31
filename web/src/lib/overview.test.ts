// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { logLevelLabel } from '#lib/logs.js';
import { m } from '#lib/paraglide/messages.js';
import {
	activePauseText,
	capRows,
	eventLevelLabel,
	eventStatus,
	formatScheduleWindow,
	instanceStatus,
	kpiFleet,
	kpiProwlarr,
	kpiSearches,
	lastRunForInstance,
	nextScheduleForInstance,
	type CapStatus,
	type Instance,
	type ProwlarrStatus,
	type Run,
	type Schedule
} from './overview.js';

const baseInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Sonarr 1',
	kind: 'sonarr',
	base_url: 'http://localhost:8989',
	source: 'manual',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z',
	last_check_at: '2026-01-01T12:00:00Z'
};

describe('overview helper: instanceStatus', () => {
	it('positive: reports ok for enabled instance with valid check and no error', () => {
		const res = instanceStatus(baseInstance);
		expect(res.status).toBe('ok');
		expect(res.label).toBeTruthy();
	});

	it('negative: reports danger for instance with error', () => {
		const res = instanceStatus({ ...baseInstance, last_error: 'Connection refused' });
		expect(res.status).toBe('danger');
		expect(res.label).toContain('Connection refused');
	});

	it('boundary: reports disabled for disabled instance', () => {
		const res = instanceStatus({ ...baseInstance, enabled: false });
		expect(res.status).toBe('disabled');
	});

	it('boundary: reports idle for never tested instance', () => {
		const res = instanceStatus({ ...baseInstance, last_check_at: undefined });
		expect(res.status).toBe('idle');
	});
});

describe('overview helper: kpiSearches', () => {
	it('positive: reports fresh global stamina with ok status', () => {
		const caps: CapStatus[] = [
			{ scope: 'global', used: 10, cap: 100, resets_at: '2026-01-01T13:00:00Z' }
		];
		const res = kpiSearches(caps);
		expect(res.status).toBe('ok');
		expect(res.level).toBe('fresh');
		expect(res.hasGlobal).toBe(true);
		expect(res.used).toBe(10);
		expect(res.cap).toBe(100);
	});

	it('negative: reports tired global stamina with warn status', () => {
		const caps: CapStatus[] = [
			{ scope: 'global', used: 80, cap: 100, resets_at: '2026-01-01T13:00:00Z' }
		];
		const res = kpiSearches(caps);
		expect(res.status).toBe('warn');
		expect(res.level).toBe('tired');
	});

	it('boundary: reports spent global stamina with danger status at 100% capacity', () => {
		const caps: CapStatus[] = [
			{ scope: 'global', used: 100, cap: 100, resets_at: '2026-01-01T13:00:00Z' }
		];
		const res = kpiSearches(caps);
		expect(res.status).toBe('danger');
		expect(res.level).toBe('spent');
	});

	it('boundary: aggregates instance caps when no global cap exists', () => {
		const caps: CapStatus[] = [
			{
				scope: 'instance',
				instance_id: '00000000-0000-0000-0000-000000000001',
				used: 15,
				cap: 50,
				resets_at: '2026-01-01T13:00:00Z'
			},
			{
				scope: 'instance',
				instance_id: '00000000-0000-0000-0000-000000000002',
				used: 25,
				cap: 50,
				resets_at: '2026-01-01T13:00:00Z'
			}
		];
		const res = kpiSearches(caps);
		expect(res.hasGlobal).toBe(false);
		expect(res.used).toBe(40);
		expect(res.status).toBe('ok');
	});

	it('boundary: handles empty or undefined caps', () => {
		const res = kpiSearches(undefined);
		expect(res.used).toBe(0);
		expect(res.hasGlobal).toBe(false);
	});
});

describe('overview helper: kpiFleet', () => {
	it('positive: reports ok when all instances are healthy', () => {
		const instances: Instance[] = [baseInstance, { ...baseInstance, id: '2', name: 'Radarr 1' }];
		const res = kpiFleet(instances);
		expect(res.status).toBe('ok');
		expect(res.total).toBe(2);
		expect(res.healthy).toBe(2);
		expect(res.failing).toBe(0);
	});

	it('negative: reports warn when at least one instance fails and one is healthy', () => {
		const instances: Instance[] = [
			baseInstance,
			{ ...baseInstance, id: '2', last_error: 'Timeout' }
		];
		const res = kpiFleet(instances);
		expect(res.status).toBe('warn');
		expect(res.healthy).toBe(1);
		expect(res.failing).toBe(1);
	});

	it('boundary: reports danger when all enabled instances fail', () => {
		const instances: Instance[] = [{ ...baseInstance, last_error: 'Fatal error' }];
		const res = kpiFleet(instances);
		expect(res.status).toBe('danger');
		expect(res.healthy).toBe(0);
		expect(res.failing).toBe(1);
	});

	it('boundary: reports idle when 0 instances exist', () => {
		const res = kpiFleet([]);
		expect(res.status).toBe('idle');
		expect(res.total).toBe(0);
	});

	it('boundary: counts disabled instances separately', () => {
		const instances: Instance[] = [baseInstance, { ...baseInstance, id: '2', enabled: false }];
		const res = kpiFleet(instances);
		expect(res.status).toBe('ok');
		expect(res.disabled).toBe(1);
		expect(res.healthy).toBe(1);
	});
});

describe('overview helper: kpiProwlarr', () => {
	type Indexer = NonNullable<ProwlarrStatus['indexers']>[number];
	const indexer = (id: number, used: number, exhausted: boolean): Indexer => ({
		id,
		name: `Idx ${id}`,
		query_limit: 100,
		limits_unit: 0,
		used_queries: used,
		used_grabs: 0,
		exhausted
	});
	const link = (indexers: Indexer[]): ProwlarrStatus => ({
		link_id: 'link1',
		name: 'Prowlarr 1',
		reachable: true,
		checked_at: '2026-01-01T00:00:00Z',
		indexers
	});

	it('positive: reports ok when all indexers have headroom', () => {
		const links: ProwlarrStatus[] = [link([indexer(1, 10, false), indexer(2, 20, false)])];
		const res = kpiProwlarr(links);
		expect(res.status).toBe('ok');
		expect(res.total).toBe(2);
		expect(res.headroom).toBe(2);
		expect(res.exhausted).toBe(0);
	});

	it('negative: reports warn when some indexers are exhausted', () => {
		const links: ProwlarrStatus[] = [link([indexer(1, 10, false), indexer(2, 100, true)])];
		const res = kpiProwlarr(links);
		expect(res.status).toBe('warn');
		expect(res.headroom).toBe(1);
		expect(res.exhausted).toBe(1);
	});

	it('boundary: reports danger when all indexers are exhausted', () => {
		const links: ProwlarrStatus[] = [link([indexer(1, 100, true)])];
		const res = kpiProwlarr(links);
		expect(res.status).toBe('danger');
		expect(res.headroom).toBe(0);
	});

	it('boundary: reports idle when no links exist', () => {
		const res = kpiProwlarr([]);
		expect(res.status).toBe('idle');
		expect(res.total).toBe(0);
	});
});

describe('overview helper: eventStatus', () => {
	it('positive: maps info to info', () => {
		expect(eventStatus('info')).toBe('info');
	});

	it('negative: maps error to danger and warn to warn', () => {
		expect(eventStatus('error')).toBe('danger');
		expect(eventStatus('warn')).toBe('warn');
	});

	it('boundary: maps debug to idle and unknown to info', () => {
		expect(eventStatus('debug')).toBe('idle');
		expect(eventStatus(undefined)).toBe('info');
		expect(eventStatus('unknown')).toBe('info');
	});
});

describe('overview helper: lastRunForInstance', () => {
	it('positive: finds run for instance', () => {
		const runs: Run[] = [
			{
				id: 'r1',
				instance_id: baseInstance.id,
				kind: 'missing',
				status: 'done',
				dry_run: false,
				queued_at: '2026-01-01T00:00:00Z',
				started_at: '2026-01-01T00:01:00Z',
				finished_at: '2026-01-01T00:05:00Z',
				searched_count: 5
			}
		];
		expect(lastRunForInstance(runs, baseInstance.id)?.id).toBe('r1');
	});

	it('boundary: returns undefined when no run matches', () => {
		expect(lastRunForInstance([], baseInstance.id)).toBeUndefined();
	});
});

describe('overview helper: nextScheduleForInstance', () => {
	const instanceSchedule: Schedule = {
		id: 's1',
		instance_id: baseInstance.id,
		name: 'Nightly',
		days: [1, 2, 3],
		start: '02:00',
		end: '04:00',
		tz: 'UTC',
		action: 'pause',
		enabled: true,
		active: false,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};

	const globalSchedule: Schedule = {
		id: 's2',
		name: 'Global Schedule',
		days: [0, 6],
		start: '12:00',
		end: '14:00',
		tz: 'UTC',
		action: 'pause',
		enabled: true,
		active: false,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};

	it('positive: prioritizes instance-specific schedule', () => {
		const res = nextScheduleForInstance([globalSchedule, instanceSchedule], baseInstance.id);
		expect(res?.id).toBe('s1');
	});

	it('positive: falls back to global schedule when instance has none', () => {
		const res = nextScheduleForInstance([globalSchedule], baseInstance.id);
		expect(res?.id).toBe('s2');
	});

	it('boundary: ignores disabled schedules', () => {
		const disabled: Schedule = { ...instanceSchedule, enabled: false };
		const res = nextScheduleForInstance([disabled], baseInstance.id);
		expect(res).toBeUndefined();
	});

	it('boundary: returns undefined when schedules list is empty or undefined', () => {
		expect(nextScheduleForInstance(undefined, baseInstance.id)).toBeUndefined();
	});
});

describe('overview helper: formatScheduleWindow', () => {
	it('positive: formats window string', () => {
		const schedule: Schedule = {
			id: 's1',
			name: 'Nightly',
			days: [1],
			start: '02:00',
			end: '04:00',
			tz: 'UTC',
			action: 'pause',
			enabled: true,
			active: false,
			created_at: '2026-01-01T00:00:00Z',
			updated_at: '2026-01-01T00:00:00Z'
		};
		expect(formatScheduleWindow(schedule)).toBe('02:00–04:00 UTC');
	});

	it('boundary: returns dash when schedule is undefined', () => {
		expect(formatScheduleWindow(undefined)).toBe('—');
	});
});

describe('overview helper: activePauseText', () => {
	const pauseSchedule: Schedule = {
		id: 's1',
		instance_id: baseInstance.id,
		name: 'Pause Sonarr',
		days: [1],
		start: '02:00',
		end: '04:00',
		tz: 'UTC',
		action: 'pause',
		enabled: true,
		active: true,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};

	it('positive: pause + active with instance_id returns text with instance name', () => {
		const res = activePauseText(pauseSchedule, [baseInstance]);
		expect(res).toBe(m.schedule_active_pause({ target: baseInstance.name }));
	});

	it('boundary: pause + active with no instance_id returns all-instances text', () => {
		const allSchedule: Schedule = { ...pauseSchedule, instance_id: undefined };
		const res = activePauseText(allSchedule, [baseInstance]);
		expect(res).toBe(m.schedule_active_pause({ target: m.schedule_all_instances() }));
	});

	it('negative: non-pause active returns empty string', () => {
		const capSchedule: Schedule = { ...pauseSchedule, action: 'cap_override', cap_value: 10 };
		const res = activePauseText(capSchedule, [baseInstance]);
		expect(res).toBe('');
	});

	it('negative: pause inactive returns empty string', () => {
		const inactiveSchedule: Schedule = { ...pauseSchedule, active: false };
		const res = activePauseText(inactiveSchedule, [baseInstance]);
		expect(res).toBe('');
	});
});

describe('overview helper: eventLevelLabel', () => {
	it('positive: maps known log levels using logLevelLabel', () => {
		expect(eventLevelLabel('debug')).toBe(logLevelLabel('debug'));
		expect(eventLevelLabel('info')).toBe(logLevelLabel('info'));
		expect(eventLevelLabel('warn')).toBe(logLevelLabel('warn'));
		expect(eventLevelLabel('error')).toBe(logLevelLabel('error'));
	});

	it('negative: returns raw string for unknown level', () => {
		expect(eventLevelLabel('custom')).toBe('custom');
		expect(eventLevelLabel('fatal')).toBe('fatal');
	});

	it('boundary: safely handles undefined without throwing', () => {
		expect(eventLevelLabel(undefined)).toBe('');
	});
});

describe('overview helper: capRows', () => {
	it('positive: keeps all rows and cut=false when list is below cap', () => {
		expect(capRows([1, 2, 3], 5)).toEqual({
			rows: [1, 2, 3],
			total: 3,
			cap: 5,
			hidden: 0,
			cut: false
		});
	});

	it('boundary: keeps all rows and cut=false when list length equals cap', () => {
		expect(capRows(['a', 'b', 'c'], 3)).toEqual({
			rows: ['a', 'b', 'c'],
			total: 3,
			cap: 3,
			hidden: 0,
			cut: false
		});
	});

	it('positive: keeps every row and counts the rows below the cap when list exceeds cap', () => {
		expect(capRows([1, 2, 3, 4, 5], 3)).toEqual({
			rows: [1, 2, 3, 4, 5],
			total: 5,
			cap: 3,
			hidden: 2,
			cut: true
		});
	});

	it('negative: handles empty and undefined lists and a negative cap', () => {
		expect(capRows([], 4)).toEqual({ rows: [], total: 0, cap: 4, hidden: 0, cut: false });
		expect(capRows(undefined, 4)).toEqual({ rows: [], total: 0, cap: 4, hidden: 0, cut: false });
		expect(capRows([1], -1)).toEqual({ rows: [1], total: 1, cap: 0, hidden: 1, cut: true });
	});
});
