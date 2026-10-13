// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SchedulesPage from './+page.svelte';

type Schedule = components['schemas']['Schedule'];

const sampleSchedule: Schedule = {
	id: '00000000-0000-0000-0000-000000000020',
	name: 'Night Quiet Hours',
	days: [1, 2, 3, 4, 5],
	start: '23:00',
	end: '07:00',
	tz: 'UTC',
	action: 'pause',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const capSchedule: Schedule = {
	id: '00000000-0000-0000-0000-000000000021',
	name: 'Peak Hours Cap',
	days: [1, 2, 3],
	start: '08:00',
	end: '18:00',
	tz: 'UTC',
	action: 'cap_override',
	cap_value: 50,
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

describe('schedules', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, table headers and schedule rows', async () => {
		stubGet({ '/schedules': [sampleSchedule, capSchedule] });
		renderWithQuery(SchedulesPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.schedules_title() })
		).toBeTruthy();

		const table = await screen.findByRole('table');
		expect(table).toBeTruthy();

		expect(within(table).getByRole('columnheader', { name: m.schedule_name() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.schedule_days() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.schedule_window() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.schedule_action() })).toBeTruthy();

		expect(within(table).getByText('Night Quiet Hours')).toBeTruthy();
		expect(within(table).getByText('Mon, Tue, Wed, Thu, Fri')).toBeTruthy();
		expect(within(table).getByText('23:00–07:00 UTC')).toBeTruthy();
		expect(within(table).getByText(m.schedule_pause())).toBeTruthy();
		const capCell = within(table).getByText(`${m.schedule_cap_override()} 50`);
		expect(capCell.textContent).toBe(`${m.schedule_cap_override()} 50`);
	});

	it('announces the problem document when the schedules query fails', async () => {
		stubGet({
			'/schedules': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'schedules query failed'
			})
		});
		renderWithQuery(SchedulesPage);

		expect((await screen.findByRole('alert')).textContent).toContain('schedules query failed');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('renders the empty state card when there are no schedules', async () => {
		stubGet({ '/schedules': [] });
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText(m.schedules_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});
});
