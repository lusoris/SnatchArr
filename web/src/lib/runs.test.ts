// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { m } from './paraglide/messages.js';
import { runStatusLabel, snatchKindLabel } from './runs.js';

describe('run labels', () => {
	it('labels every run status of the API', () => {
		expect(['queued', 'leased', 'done', 'failed', 'cancelled'].map(runStatusLabel)).toEqual([
			m.run_status_queued(),
			m.run_status_leased(),
			m.run_status_done(),
			m.run_status_failed(),
			m.run_status_cancelled()
		]);
	});

	it('labels both snatch kinds', () => {
		expect(snatchKindLabel('missing')).toBe(m.snatch_kind_missing());
		expect(snatchKindLabel('upgrade')).toBe(m.snatch_kind_upgrade());
	});

	it('shows an unknown value as sent, including inherited property names', () => {
		expect(runStatusLabel('paused')).toBe('paused');
		expect(runStatusLabel('toString')).toBe('toString');
		expect(snatchKindLabel('')).toBe('');
	});
});
