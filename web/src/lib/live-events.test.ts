// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { LIVE_EVENTS } from './live-events.js';

describe('LIVE_EVENTS', () => {
	it('names the events the API records itself', () => {
		expect(LIVE_EVENTS).toContain('run_queued');
		expect(LIVE_EVENTS).toContain('events_unconfirmed');
	});

	it('holds only lower snake case names without the proto prefix', () => {
		for (const name of LIVE_EVENTS) {
			expect(name).toMatch(/^[a-z]+(_[a-z]+)*$/);
		}
		expect(LIVE_EVENTS).not.toContain('message');
	});

	it('lists every name once', () => {
		expect(new Set(LIVE_EVENTS).size).toBe(LIVE_EVENTS.length);
	});
});
