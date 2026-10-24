// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import HistoryTab from './HistoryTab.svelte';

type Event = components['schemas']['Event'];

const sampleEvent: Event = {
	id: 101,
	instance_id: '00000000-0000-0000-0000-000000000001',
	ts: '2026-09-01T12:00:00Z',
	level: 'info',
	type: 'search_dispatched',
	title: 'Sample Event Title',
	detail: 'Searched 5 items'
};

describe('history', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, table headers and event rows in data grid', async () => {
		stubGet({ '/events': [sampleEvent] });
		renderWithQuery(HistoryTab);

		expect(await screen.findByRole('heading', { level: 2, name: m.history_title() })).toBeTruthy();

		const grid = await screen.findByRole('grid', { name: m.history_title() });
		expect(grid).toBeTruthy();

		expect(within(grid).getByRole('columnheader', { name: m.history_when() })).toBeTruthy();
		expect(within(grid).getByRole('columnheader', { name: m.history_level() })).toBeTruthy();
		expect(within(grid).getByRole('columnheader', { name: m.history_type() })).toBeTruthy();
		expect(within(grid).getByRole('columnheader', { name: m.history_title_col() })).toBeTruthy();
		expect(within(grid).getByRole('columnheader', { name: m.history_detail() })).toBeTruthy();

		expect(within(grid).getByText('Sample Event Title')).toBeTruthy();
		expect(within(grid).getByText('Searched 5 items')).toBeTruthy();
		expect(within(grid).getByText('info')).toBeTruthy();
		expect(within(grid).getByText('search_dispatched')).toBeTruthy();
	});

	it('announces the problem document when the events query fails', async () => {
		stubGet({
			'/events': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'history store failed'
			})
		});
		renderWithQuery(HistoryTab);

		expect((await screen.findByRole('alert')).textContent).toContain('history store failed');
		expect(screen.queryByRole('grid')).toBeNull();
	});

	it('renders empty data grid when there are no events', async () => {
		stubGet({ '/events': [] });
		renderWithQuery(HistoryTab);

		const grid = await screen.findByRole('grid', { name: m.history_title() });
		expect(grid).toBeTruthy();
		expect(within(grid).queryAllByRole('gridcell')).toHaveLength(0);
	});
});
