// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { eventLevelLabel } from '#lib/overview.js';
import { formatWhen } from '#lib/stamina.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import EventDialog from './EventDialog.svelte';

const event: components['schemas']['Event'] = {
	id: 1,
	instance_id: 'instance-1',
	run_id: 'run-1',
	type: 'search_dispatched',
	title: 'Example movie',
	level: 'info',
	ts: '2026-10-28T12:00:00Z',
	detail: 'Search accepted'
};

describe('EventDialog', () => {
	afterEach(() => vi.restoreAllMocks());

	it('renders fields, payload and instance href', async () => {
		stubGet({ '/instances': [{ ...forkInstance, id: 'instance-1' }] });
		renderWithQuery(EventDialog, { event, open: true });
		const dialog = within(screen.getByRole('dialog'));
		expect((await dialog.findByRole('link', { name: forkInstance.name })).textContent).toBe(
			forkInstance.name
		);
		expect(dialog.getByText(event.type)).toBeTruthy();
		expect(dialog.getByText(event.title!)).toBeTruthy();
		expect(dialog.getByText(eventLevelLabel(event.level))).toBeTruthy();
		expect(dialog.getByText(formatWhen(event.ts, getLocale()))).toBeTruthy();
		expect(dialog.getByRole('link').getAttribute('href')).toBe('/instances/instance-1');
		expect(dialog.getByRole('button', { name: event.run_id })).toBeTruthy();
		expect(dialog.getByText(/"detail": "Search accepted"/)).toBeTruthy();
	});

	it('shows the problem from the linked run', async () => {
		stubGet({
			'/instances': [],
			'/runs/{runId}': new ProblemError({
				type: 'about:blank',
				status: 404,
				detail: 'Run no longer exists'
			})
		});
		renderWithQuery(EventDialog, { event, open: true });
		await fireEvent.click(screen.getByRole('button', { name: event.run_id }));
		expect((await screen.findByRole('alert')).textContent).toContain('Run no longer exists');
	});

	it('renders no instance link or run button for an unassociated event', () => {
		const unassociated = { ...event, run_id: undefined };
		renderWithQuery(EventDialog, { event: { ...unassociated, instance_id: '' }, open: true });
		const dialog = within(screen.getByRole('dialog'));
		expect(dialog.queryByRole('link')).toBeNull();
		expect(dialog.queryByRole('button', { name: 'run-1' })).toBeNull();
		expect(dialog.getAllByRole('button')).toHaveLength(1);
	});
});
