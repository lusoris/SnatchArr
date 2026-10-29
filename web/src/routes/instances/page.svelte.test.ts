// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import {
	assertCancelDelete,
	failedInstance,
	forkInstance,
	renderWithQuery,
	stubGet,
	untestedInstance
} from '#lib/testing/harness.js';
import InstancesPage from './+page.svelte';

/** The text of every cell in the body rows of the instance table. */
async function bodyRows(): Promise<string[][]> {
	const table = await screen.findByRole('table');
	return within(table)
		.getAllByRole('row')
		.slice(1)
		.map((row) =>
			within(row)
				.getAllByRole('cell')
				.map((cell) => cell.textContent?.trim() ?? '')
		);
}

describe('instance list', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows name, kind with the fork, address and passed connection test of an instance', async () => {
		stubGet({ '/instances': [forkInstance] });
		renderWithQuery(InstancesPage);

		const rows = await bodyRows();
		expect(rows).toHaveLength(1);
		expect(rows[0]?.slice(0, 3)).toEqual([
			'Library Readarr',
			'Readarr · Bookshelf',
			'http://readarr.local:8787'
		]);
		expect(rows[0]?.[5]).toContain('0.4.21.182');
		expect(screen.getByRole('columnheader', { name: m.instance_last_check() })).toBeTruthy();
		const links = screen.getAllByRole('link', { name: 'Library Readarr' });
		expect(links.length).toBeGreaterThan(0);
		expect(links[0].getAttribute('href')).toBe(`/instances/${forkInstance.id}`);
	});

	it('shows failed connection test error in the table', async () => {
		stubGet({ '/instances': [failedInstance] });
		renderWithQuery(InstancesPage);

		const rows = await bodyRows();
		expect(rows).toHaveLength(1);
		expect(rows[0]?.[5]).toContain('Connection refused (ECONNREFUSED)');
		const statuses = screen.getAllByRole('status');
		expect(statuses.some((s) => s.textContent?.includes('Connection refused (ECONNREFUSED)'))).toBe(
			true
		);
	});

	it('shows never tested state in the table', async () => {
		stubGet({ '/instances': [untestedInstance] });
		renderWithQuery(InstancesPage);

		const rows = await bodyRows();
		expect(rows).toHaveLength(1);
		expect(rows[0]?.[5]).toBe(m.instance_never_tested());
		expect(screen.getAllByText(m.instance_never_tested()).length).toBeGreaterThan(0);
	});

	it('shows stacked card list with instance details and delete button below md', async () => {
		stubGet({ '/instances': [forkInstance] });
		renderWithQuery(InstancesPage);

		const list = await screen.findByRole('list', { name: m.nav_instances() });
		expect(list).toBeTruthy();
		const items = within(list).getAllByRole('listitem');
		expect(items).toHaveLength(1);
		expect(items[0].textContent).toContain('Library Readarr');
		expect(items[0].textContent).toContain('Readarr · Bookshelf');
		expect(items[0].textContent).toContain('http://readarr.local:8787');
		expect(items[0].textContent).toContain('manual');
		expect(within(items[0]).getByRole('button', { name: m.action_delete() })).toBeTruthy();
	});

	it('deletes an instance only after the in-page confirmation', async () => {
		stubGet({ '/instances': [forkInstance] });
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(InstancesPage);

		const table = await screen.findByRole('table');
		const row = within(within(table).getAllByRole('row')[1]);
		await fireEvent.click(row.getByRole('button', { name: m.action_delete() }));
		expect(deleteSpy).not.toHaveBeenCalled();
		const question = row.getByRole('group', {
			name: m.confirm_delete({ name: forkInstance.name })
		});
		await fireEvent.click(within(question).getByRole('button', { name: m.confirm_delete_yes() }));
		expect(deleteSpy).toHaveBeenCalledWith('/instances/{instanceId}', {
			params: { path: { instanceId: forkInstance.id } }
		});
	});

	it('keeps an instance when the delete question in its card is cancelled', async () => {
		stubGet({ '/instances': [forkInstance] });
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(InstancesPage);

		const list = await screen.findByRole('list', { name: m.nav_instances() });
		await assertCancelDelete(within(within(list).getAllByRole('listitem')[0]), deleteSpy);
	});

	it('shows the table without a body row when there is no instance', async () => {
		stubGet({ '/instances': [] });
		renderWithQuery(InstancesPage);

		expect(await bodyRows()).toEqual([]);
	});

	it('offers to reset the processed memory of every instance', async () => {
		stubGet({ '/instances': [forkInstance] });
		renderWithQuery(InstancesPage);

		await bodyRows();
		expect(screen.getByRole('button', { name: m.memory_reset_all() })).toBeTruthy();
	});

	it('announces the problem document when the instance list is refused', async () => {
		stubGet({
			'/instances': new ProblemError({ type: 'about:blank', status: 503, detail: 'store down' })
		});
		renderWithQuery(InstancesPage);

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('table')).toBeNull();
	});
});
