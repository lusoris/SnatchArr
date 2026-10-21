// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
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
	active: false,
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
	active: false,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const baseStubs = {
	'/schedules': [sampleSchedule, capSchedule],
	'/instances': []
};

async function clickFirstButton(name: string): Promise<void> {
	const buttons = await screen.findAllByRole('button', { name });
	const first = buttons[0];
	if (!first) throw new Error(`missing button: ${name}`);
	await fireEvent.click(first);
}

async function openFirstEdit(): Promise<void> {
	await clickFirstButton(m.schedule_edit());
	expect(
		await screen.findByRole('heading', { level: 2, name: m.schedule_edit_title() })
	).toBeTruthy();
}

async function openFirstDeleteConfirm(): Promise<void> {
	await clickFirstButton(m.schedule_delete());
	expect(screen.getByText(m.schedule_delete_confirm({ name: sampleSchedule.name }))).toBeTruthy();
}

describe('schedules', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, table headers and schedule rows', async () => {
		stubGet(baseStubs);
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

	it('shows active pause indicator with target name or all instances', async () => {
		const activeGlobal: Schedule = {
			...sampleSchedule,
			id: '00000000-0000-0000-0000-000000000022',
			name: 'Global Pause Active',
			active: true,
			action: 'pause'
		};
		const activeInstance: Schedule = {
			...sampleSchedule,
			id: '00000000-0000-0000-0000-000000000023',
			name: 'Readarr Pause Active',
			instance_id: forkInstance.id,
			active: true,
			action: 'pause'
		};
		stubGet({
			'/schedules': [activeGlobal, activeInstance],
			'/instances': [forkInstance]
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText('Pausing: all instances')).toBeTruthy();
		expect(screen.getByText('Pausing: Library Readarr')).toBeTruthy();
	});

	it('announces the problem document when the schedules query fails', async () => {
		stubGet({
			'/schedules': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'schedules query failed'
			}),
			'/instances': []
		});
		renderWithQuery(SchedulesPage);

		expect((await screen.findByRole('alert')).textContent).toContain('schedules query failed');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('renders the empty state card when there are no schedules', async () => {
		stubGet({ '/schedules': [], '/instances': [] });
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText(m.schedules_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('creates a new schedule on form submit', async () => {
		stubGet({ '/schedules': [], '/instances': [] });
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: { ...sampleSchedule, id: '00000000-0000-0000-0000-000000000024' }
		} as never);
		renderWithQuery(SchedulesPage);

		const nameInput = await screen.findByLabelText(m.schedule_name());
		const startInput = screen.getByLabelText(m.schedule_start());
		const endInput = screen.getByLabelText(m.schedule_end());
		const tzInput = screen.getByLabelText(m.schedule_tz());
		const submitBtn = screen.getByRole('button', { name: m.schedule_add() });

		await fireEvent.input(nameInput, { target: { value: 'New Test Schedule' } });
		await fireEvent.input(startInput, { target: { value: '01:00' } });
		await fireEvent.input(endInput, { target: { value: '05:00' } });
		await fireEvent.input(tzInput, { target: { value: 'UTC' } });
		await fireEvent.click(submitBtn);

		expect(postSpy).toHaveBeenCalledWith('/schedules', {
			body: {
				name: 'New Test Schedule',
				days: [0, 1, 2, 3, 4, 5, 6],
				start: '01:00',
				end: '05:00',
				tz: 'UTC',
				action: 'pause',
				enabled: true
			}
		});
	});

	it('edits an existing schedule and submits updates', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { ...sampleSchedule, name: 'Renamed Night Hours' }
		} as never);
		renderWithQuery(SchedulesPage);
		await openFirstEdit();

		const nameInput = screen.getByLabelText(m.schedule_name());
		expect((nameInput as HTMLInputElement).value).toBe('Night Quiet Hours');
		await fireEvent.input(nameInput, { target: { value: 'Renamed Night Hours' } });

		const saveBtn = screen.getByRole('button', { name: m.schedule_save() });
		await fireEvent.click(saveBtn);

		expect(putSpy).toHaveBeenCalledWith('/schedules/{scheduleId}', {
			params: { path: { scheduleId: sampleSchedule.id } },
			body: {
				name: 'Renamed Night Hours',
				days: [1, 2, 3, 4, 5],
				start: '23:00',
				end: '07:00',
				tz: 'UTC',
				action: 'pause',
				enabled: true
			}
		});
	});

	it('cancels editing and resets form to add mode', async () => {
		stubGet(baseStubs);
		renderWithQuery(SchedulesPage);
		await openFirstEdit();

		const cancelBtn = screen.getByRole('button', { name: m.schedule_cancel() });
		await fireEvent.click(cancelBtn);

		expect(await screen.findByRole('heading', { level: 2, name: m.schedule_add() })).toBeTruthy();
	});

	it('deletes a schedule with explicit in-page confirmation', async () => {
		stubGet(baseStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(SchedulesPage);
		await openFirstDeleteConfirm();

		const confirmBtn = screen.getByRole('button', { name: m.schedule_confirm_delete() });
		await fireEvent.click(confirmBtn);

		expect(deleteSpy).toHaveBeenCalledWith('/schedules/{scheduleId}', {
			params: { path: { scheduleId: sampleSchedule.id } }
		});
	});

	it('cancels delete confirmation without dispatching DELETE', async () => {
		stubGet(baseStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(SchedulesPage);
		await openFirstDeleteConfirm();

		const cancelBtn = screen.getByRole('button', { name: m.schedule_cancel() });
		await fireEvent.click(cancelBtn);

		expect(screen.queryByText(m.schedule_delete_confirm({ name: sampleSchedule.name }))).toBeNull();
		expect(deleteSpy).not.toHaveBeenCalled();
	});

	it('shows cap_value input only when action is cap_override', async () => {
		stubGet({ '/schedules': [], '/instances': [] });
		renderWithQuery(SchedulesPage);

		expect(screen.queryByLabelText(m.schedule_cap_value())).toBeNull();

		const actionSelect = await screen.findByLabelText(m.schedule_action());
		await fireEvent.change(actionSelect, { target: { value: 'cap_override' } });

		expect(screen.getByLabelText(m.schedule_cap_value())).toBeTruthy();

		await fireEvent.change(actionSelect, { target: { value: 'pause' } });
		expect(screen.queryByLabelText(m.schedule_cap_value())).toBeNull();
	});

	it('enforces validation boundaries for start/end pattern and cap_value', async () => {
		stubGet({ '/schedules': [], '/instances': [] });
		renderWithQuery(SchedulesPage);

		const startInput = (await screen.findByLabelText(m.schedule_start())) as HTMLInputElement;
		await fireEvent.input(startInput, { target: { value: '25:00' } });
		expect(startInput.checkValidity()).toBe(false);
		expect(startInput.validity.patternMismatch).toBe(true);

		await fireEvent.input(startInput, { target: { value: '08:30' } });
		expect(startInput.checkValidity()).toBe(true);

		const endInput = screen.getByLabelText(m.schedule_end()) as HTMLInputElement;
		await fireEvent.input(endInput, { target: { value: 'invalid' } });
		expect(endInput.checkValidity()).toBe(false);

		const actionSelect = screen.getByLabelText(m.schedule_action());
		await fireEvent.change(actionSelect, { target: { value: 'cap_override' } });

		const capInput = screen.getByLabelText(m.schedule_cap_value()) as HTMLInputElement;
		await fireEvent.input(capInput, { target: { value: '1' } });
		expect(capInput.checkValidity()).toBe(true);

		await fireEvent.input(capInput, { target: { value: '500' } });
		expect(capInput.checkValidity()).toBe(true);

		await fireEvent.input(capInput, { target: { value: '0' } });
		expect(capInput.checkValidity()).toBe(false);
		expect(capInput.validity.rangeUnderflow).toBe(true);

		await fireEvent.input(capInput, { target: { value: '501' } });
		expect(capInput.checkValidity()).toBe(false);
		expect(capInput.validity.rangeOverflow).toBe(true);
	});
});
