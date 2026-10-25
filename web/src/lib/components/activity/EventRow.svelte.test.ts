// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import type { LiveFrame } from '#lib/live.js';
import EventRow from './EventRow.svelte';

describe('EventRow', () => {
	const sampleEvent: LiveFrame = {
		id: 42,
		run_id: '11111111-1111-1111-1111-111111111111',
		instance_id: 'inst-1',
		level: 'warn',
		type: 'queue_item_struck',
		event: 'queue_item_struck',
		title: 'Item Struck',
		detail: 'Import blocked for 3 tries',
		ts: '2026-10-25T10:00:00Z'
	};

	it('positive: Enter and Space toggle aria-expanded and detail JSON view', async () => {
		render(EventRow, {
			props: { event: sampleEvent, instanceName: 'Sonarr Main' }
		});

		const btn = screen.getByRole('button', { name: /Item Struck/ });
		expect(btn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText(/"queue_item_struck"/)).toBeNull();

		// Press Enter -> expands
		await fireEvent.keyDown(btn, { key: 'Enter' });
		expect(btn.getAttribute('aria-expanded')).toBe('true');
		expect(screen.getByText(/"queue_item_struck"/)).toBeTruthy();

		// Press Space -> collapses
		await fireEvent.keyDown(btn, { key: ' ' });
		expect(btn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText(/"queue_item_struck"/)).toBeNull();
	});

	it('negative: non-toggle keys like ArrowDown do not alter aria-expanded', async () => {
		render(EventRow, {
			props: { event: sampleEvent }
		});

		const btn = screen.getByRole('button', { name: /Item Struck/ });
		expect(btn.getAttribute('aria-expanded')).toBe('false');

		await fireEvent.keyDown(btn, { key: 'ArrowDown' });
		expect(btn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText(/"queue_item_struck"/)).toBeNull();
	});

	it('boundary: minimal event with missing title and detail renders without throwing', () => {
		const minimal: LiveFrame = {
			id: 99,
			type: 'heartbeat',
			event: 'heartbeat',
			ts: '2026-10-25T12:00:00Z'
		};

		render(EventRow, {
			props: { event: minimal }
		});

		expect(screen.getByText('heartbeat')).toBeTruthy();
	});
});
