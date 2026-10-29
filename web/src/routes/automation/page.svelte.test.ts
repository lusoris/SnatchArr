// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import {
	expectStackedLabels,
	forkInstance,
	renderWithQuery,
	stubGet
} from '#lib/testing/harness.js';
import SchedulesPage from './+page.svelte';

type Schedule = components['schemas']['Schedule'];

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/automation')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

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

const mediaFirstRanks = {
	sonarr: 10,
	radarr: 10,
	lidarr: 20,
	readarr: 30,
	whisparr_v2: 40,
	whisparr_v3: 40
};

const sampleSettings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	search_priority_preset: mediaFirstRanks,
	download_priority_preset: mediaFirstRanks,
	llm_enabled: false,
	llm_write_tools: [],
	updated_at: '2026-10-25T05:00:00Z'
};

const baseStubs = {
	'/schedules': [sampleSchedule, capSchedule],
	'/instances': [],
	'/settings': sampleSettings,
	'/priority-presets': [],
	'/system/modules': []
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

describe('automation page tabs', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/automation');
		session.state = 'authenticated';
		stubGet(baseStubs);
	});

	afterEach(() => {
		vi.restoreAllMocks();
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		session.state = 'authenticated';
	});

	it('renders schedules tab by default', async () => {
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_automation() })).toBeTruthy();
		expect(
			screen.getByRole('tab', { name: m.automation_tab_schedules(), selected: true })
		).toBeTruthy();
		expect(
			screen.getByRole('tab', { name: m.automation_tab_caps(), selected: false })
		).toBeTruthy();
		expect(await screen.findByRole('table')).toBeTruthy();
	});

	it('deep link: renders caps tab when tab=caps', async () => {
		pageMock.url = new URL('http://localhost/automation?tab=caps');
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_automation() })).toBeTruthy();
		expect(screen.getByRole('tab', { name: m.automation_tab_caps(), selected: true })).toBeTruthy();
		expect(
			screen.getByRole('tab', { name: m.automation_tab_schedules(), selected: false })
		).toBeTruthy();
		expect(await screen.findByLabelText(m.settings_global_cap())).toBeTruthy();
	});

	it('unknown tab param falls back to schedules tab', async () => {
		pageMock.url = new URL('http://localhost/automation?tab=unknown');
		renderWithQuery(SchedulesPage);

		expect(
			screen.getByRole('tab', { name: m.automation_tab_schedules(), selected: true })
		).toBeTruthy();
		expect(await screen.findByRole('table')).toBeTruthy();
	});

	it('clicking caps tab calls goto with tab=caps', async () => {
		renderWithQuery(SchedulesPage);

		const capsTab = screen.getByRole('tab', { name: m.automation_tab_caps() });
		await fireEvent.click(capsTab);

		expect(navMocks.goto).toHaveBeenCalledWith('/automation?tab=caps', { replaceState: true });
	});

	it('viewer role: schedules tab shows table without edit/delete buttons or form', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('table')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.schedule_add() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.schedule_edit() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.schedule_delete() })).toBeNull();
	});

	it('viewer role: caps tab shows disabled inputs and no save button', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		pageMock.url = new URL('http://localhost/automation?tab=caps');
		renderWithQuery(SchedulesPage);

		const cap = (await screen.findByLabelText(m.settings_global_cap())) as HTMLInputElement;
		expect(cap.disabled).toBe(true);
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('negative: season-gate tab hidden when watchaware module disabled and tab param falls back', async () => {
		pageMock.url = new URL('http://localhost/automation?tab=season-gate');
		stubGet({
			...baseStubs,
			'/system/modules': [{ id: 'watchaware', enabled: false }]
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_automation() })).toBeTruthy();
		expect(screen.queryByRole('tab', { name: m.watchaware_gate_tab() })).toBeNull();
		expect(
			screen.getByRole('tab', { name: m.automation_tab_schedules(), selected: true })
		).toBeTruthy();
	});

	it('positive: season-gate tab visible and activated when watchaware module enabled', async () => {
		pageMock.url = new URL('http://localhost/automation?tab=season-gate');
		stubGet({
			...baseStubs,
			'/system/modules': [{ id: 'watchaware', enabled: true }],
			'/watchaware/gate/series': [],
			'/watchaware/gate/decisions': []
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_automation() })).toBeTruthy();
		expect(
			await screen.findByRole('tab', { name: m.watchaware_gate_tab(), selected: true })
		).toBeTruthy();
		expect(await screen.findByText(m.watchaware_gate_observe_note())).toBeTruthy();
	});
});

describe('schedules', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/automation');
	});

	afterEach(() => {
		vi.restoreAllMocks();
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	});

	it('renders heading, table headers and schedule rows', async () => {
		stubGet(baseStubs);
		renderWithQuery(SchedulesPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_automation() })).toBeTruthy();

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
			'/instances': [forkInstance],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText('Pausing: all instances')).toBeTruthy();
		expect(screen.getByText('Pausing: Library Readarr')).toBeTruthy();
	});

	it('shows empty message when no schedules exist', async () => {
		stubGet({
			'/schedules': [],
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText(m.schedules_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('announces the problem document when the schedules query fails', async () => {
		stubGet({
			'/schedules': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'schedules query failed'
			}),
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		expect((await screen.findByRole('alert')).textContent).toContain('schedules query failed');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('creates a new schedule on form submit', async () => {
		stubGet({
			'/schedules': [],
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
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
		stubGet({
			'/schedules': [],
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		expect(screen.queryByLabelText(m.schedule_cap_value())).toBeNull();

		const actionSelect = await screen.findByLabelText(m.schedule_action());
		await fireEvent.change(actionSelect, { target: { value: 'cap_override' } });

		expect(screen.getByLabelText(m.schedule_cap_value())).toBeTruthy();

		await fireEvent.change(actionSelect, { target: { value: 'pause' } });
		expect(screen.queryByLabelText(m.schedule_cap_value())).toBeNull();
	});

	it('enforces validation boundaries for start/end pattern and cap_value', async () => {
		stubGet({
			'/schedules': [],
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
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

	it('stacks schedules table rows below md with data-label matching th on every td except actions', async () => {
		stubGet({
			'/schedules': [sampleSchedule],
			'/instances': [forkInstance],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		const table = await screen.findByRole('table');
		expectStackedLabels(table);
	});

	it('renders no schedules table rows when schedule list is empty', async () => {
		stubGet({
			'/schedules': [],
			'/instances': [],
			'/settings': sampleSettings,
			'/priority-presets': []
		});
		renderWithQuery(SchedulesPage);

		expect(await screen.findByText(m.schedules_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('positive: admin schedule row has class target and edit button has class target-link', async () => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet(baseStubs);
		renderWithQuery(SchedulesPage);

		const table = await screen.findByRole('table');
		const row = within(table).getByText('Night Quiet Hours').closest('tr') as HTMLTableRowElement;
		expect(row.classList.contains('target')).toBe(true);

		const editBtn = within(row).getByRole('button', { name: m.schedule_edit() });
		expect(editBtn.classList.contains('target-link')).toBe(true);
	});

	it('negative: viewer schedule row has no class target', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet(baseStubs);
		renderWithQuery(SchedulesPage);

		const table = await screen.findByRole('table');
		const rows = table.querySelectorAll('tbody tr');
		expect(rows.length).toBeGreaterThan(0);
		for (const row of rows) {
			expect(row.classList.contains('target')).toBe(false);
		}
	});

	it('negative: schedule row loses class target when in deleting confirmation state', async () => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet(baseStubs);
		renderWithQuery(SchedulesPage);

		const table = await screen.findByRole('table');
		const row = within(table).getByText('Night Quiet Hours').closest('tr') as HTMLTableRowElement;
		expect(row.classList.contains('target')).toBe(true);

		await openFirstDeleteConfirm();

		expect(row.classList.contains('target')).toBe(false);
	});
});
