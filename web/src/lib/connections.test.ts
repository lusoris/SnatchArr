// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	deriveConfigarrStatus,
	deriveLinkStatus,
	deriveServiceStatus,
	type HealthItem
} from './connections.js';
import { m } from '#lib/paraglide/messages.js';

describe('deriveServiceStatus', () => {
	it('positive: all healthy items return ok status and counts', () => {
		const items: HealthItem[] = [
			{ reachable: true, error: null },
			{ reachable: true, checkedAt: '2026-10-25T05:00:00Z' }
		];
		const res = deriveServiceStatus(items);

		expect(res.status).toBe('ok');
		expect(res.counts).toEqual({ total: 2, healthy: 2, failing: 0, untested: 0 });
		expect(res.label).toBe(m.connections_health_ok());
		expect(res.countLine).toBe(m.connections_status_linked({ count: 2 }));
	});

	it('negative: all failing items return danger status and countLine', () => {
		const items: HealthItem[] = [
			{ reachable: false, error: 'connection refused' },
			{ error: 'timeout' }
		];
		const res = deriveServiceStatus(items);

		expect(res.status).toBe('danger');
		expect(res.counts).toEqual({ total: 2, healthy: 0, failing: 2, untested: 0 });
		expect(res.label).toBe(m.connections_health_danger());
		expect(res.countLine).toBe(m.connections_status_failing({ total: 2, failing: 2 }));
	});

	it('boundary: empty list returns idle status with not linked label', () => {
		const res = deriveServiceStatus([]);

		expect(res.status).toBe('idle');
		expect(res.counts).toEqual({ total: 0, healthy: 0, failing: 0, untested: 0 });
		expect(res.label).toBe(m.connections_health_not_linked());
		expect(res.countLine).toBe('');
	});

	it('boundary: mixed healthy and failing items return warn status', () => {
		const items: HealthItem[] = [
			{ reachable: true, error: null },
			{ reachable: false, error: 'timed out' },
			{ checkedAt: '2026-10-25T04:00:00Z' }
		];
		const res = deriveServiceStatus(items);

		expect(res.status).toBe('warn');
		expect(res.counts).toEqual({ total: 3, healthy: 2, failing: 1, untested: 0 });
		expect(res.label).toBe(m.connections_health_warn());
		expect(res.countLine).toBe(m.connections_status_failing({ total: 3, failing: 1 }));
	});

	it('boundary: items without check results return idle status with never tested label', () => {
		const items: HealthItem[] = [
			{ reachable: null, error: null, checkedAt: null },
			{ reachable: undefined, error: undefined }
		];
		const res = deriveServiceStatus(items);

		expect(res.status).toBe('idle');
		expect(res.counts).toEqual({ total: 2, healthy: 0, failing: 0, untested: 2 });
		expect(res.label).toBe(m.instance_never_tested());
		expect(res.countLine).toBe(m.connections_status_linked({ count: 2 }));
	});
});

describe('deriveLinkStatus', () => {
	it('positive: checked link without error returns ok status', () => {
		const res = deriveLinkStatus({ last_check_at: '2026-10-25T05:00:00Z', last_error: null });
		expect(res.status).toBe('ok');
		expect(res.label).toBe(m.connections_health_ok());
	});

	it('negative: link with last_error returns danger status with error label', () => {
		const res = deriveLinkStatus({
			last_check_at: '2026-10-25T05:00:00Z',
			last_error: 'HTTP 401 Unauthorized'
		});
		expect(res.status).toBe('danger');
		expect(res.label).toBe('HTTP 401 Unauthorized');
	});

	it('boundary: link without check or error returns idle status', () => {
		const res = deriveLinkStatus({ last_check_at: null, last_error: null });
		expect(res.status).toBe('idle');
		expect(res.label).toBe(m.instance_never_tested());
	});
});

describe('deriveConfigarrStatus', () => {
	it('positive: linked configarr with import timestamp returns ok status', () => {
		const res = deriveConfigarrStatus(
			{
				linked: true,
				last_import_at: '2026-10-25T05:00:00Z',
				last_error: null
			},
			'en'
		);
		expect(res.status).toBe('ok');
		expect(res.counts).toEqual({ total: 1, healthy: 1, failing: 0, untested: 0 });
		expect(res.label).toBe(m.connections_health_ok());
		expect(res.countLine).toContain('Linked mode');
	});

	it('negative: linked configarr with last_error returns danger status', () => {
		const res = deriveConfigarrStatus(
			{
				linked: true,
				last_import_at: '2026-10-25T05:00:00Z',
				last_error: 'YAML parse error at line 12'
			},
			'en'
		);
		expect(res.status).toBe('danger');
		expect(res.counts).toEqual({ total: 1, healthy: 0, failing: 1, untested: 0 });
		expect(res.label).toBe(m.connections_health_danger());
	});

	it('boundary: unlinked configarr returns idle status', () => {
		const res = deriveConfigarrStatus({ linked: false }, 'en');
		expect(res.status).toBe('idle');
		expect(res.counts).toEqual({ total: 0, healthy: 0, failing: 0, untested: 0 });
		expect(res.label).toBe(m.connections_health_not_linked());
		expect(res.countLine).toBe('');
	});

	it('boundary: linked configarr with no previous import returns idle status', () => {
		const res = deriveConfigarrStatus(
			{ linked: true, last_import_at: null, last_error: null },
			'en'
		);
		expect(res.status).toBe('idle');
		expect(res.counts).toEqual({ total: 1, healthy: 0, failing: 0, untested: 1 });
		expect(res.label).toBe(m.instance_never_tested());
		expect(res.countLine).toBe(m.connections_configarr_linked_no_import());
	});
});
