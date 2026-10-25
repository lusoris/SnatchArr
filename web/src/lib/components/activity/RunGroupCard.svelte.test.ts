// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import type { ActivityGroup } from '#lib/activity.js';
import { m } from '#lib/paraglide/messages.js';
import RunGroupCard from './RunGroupCard.svelte';

describe('RunGroupCard', () => {
	const sampleGroup: ActivityGroup = {
		id: 'group-1',
		isGroup: true,
		run_id: '11111111-1111-1111-1111-111111111111',
		instance_id: 'inst-1',
		type: 'run_started',
		events: [
			{
				id: 1,
				type: 'run_started',
				event: 'run_started',
				title: 'Start',
				ts: '2026-10-25T10:00:00Z'
			},
			{
				id: 2,
				type: 'search_dispatched',
				event: 'search_dispatched',
				title: 'Search',
				detail: '5 items',
				ts: '2026-10-25T10:00:10Z'
			}
		],
		itemCount: 5,
		dispatches: 1,
		level: 'info',
		status: 'ok',
		ts: '2026-10-25T10:00:10Z'
	};

	it('positive: renders summary count of dispatches and items, and toggles child events', async () => {
		render(RunGroupCard, {
			props: { group: sampleGroup, instanceName: 'Sonarr' }
		});

		const summaryText = m.activity_run_summary({ dispatches: 1, items: 5 });
		expect(screen.getByText(summaryText)).toBeTruthy();

		const toggleBtn = screen.getByRole('button', { name: /Run 11111111/ });
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText('Start')).toBeNull();

		await fireEvent.click(toggleBtn);
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('true');
		expect(screen.getByText('Start')).toBeTruthy();
		expect(screen.getByText('Search')).toBeTruthy();
	});

	it('negative: collapsed group does not render child events', () => {
		render(RunGroupCard, {
			props: { group: sampleGroup }
		});

		const toggleBtn = screen.getByRole('button', { name: /Run 11111111/ });
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText('Start')).toBeNull();
	});

	it('boundary: group with 0 dispatches and 0 items formats correctly', () => {
		const emptyGroup: ActivityGroup = {
			id: 'group-empty',
			isGroup: true,
			events: [],
			itemCount: 0,
			dispatches: 0,
			level: 'info',
			status: 'idle',
			type: 'idle_run'
		};

		render(RunGroupCard, {
			props: { group: emptyGroup }
		});

		const summaryText = m.activity_run_summary({ dispatches: 0, items: 0 });
		expect(screen.getByText(summaryText)).toBeTruthy();
	});
});
