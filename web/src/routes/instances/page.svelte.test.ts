// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import {
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
			'readarr · Bookshelf',
			'http://readarr.local:8787'
		]);
		expect(rows[0]?.[5]).toContain('0.4.21.182');
		expect(screen.getByRole('columnheader', { name: m.instance_last_check() })).toBeTruthy();
		const link = screen.getByRole('link', { name: 'Library Readarr' });
		expect(link.getAttribute('href')).toBe(`/instances/${forkInstance.id}`);
	});

	it('shows failed connection test error in the table', async () => {
		stubGet({ '/instances': [failedInstance] });
		renderWithQuery(InstancesPage);

		const rows = await bodyRows();
		expect(rows).toHaveLength(1);
		expect(rows[0]?.[5]).toContain('Connection refused (ECONNREFUSED)');
		expect(screen.getByRole('status').textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('shows never tested state in the table', async () => {
		stubGet({ '/instances': [untestedInstance] });
		renderWithQuery(InstancesPage);

		const rows = await bodyRows();
		expect(rows).toHaveLength(1);
		expect(rows[0]?.[5]).toBe(m.instance_never_tested());
		expect(screen.getByText(m.instance_never_tested())).toBeTruthy();
	});

	it('shows the table without a body row when there is no instance', async () => {
		stubGet({ '/instances': [] });
		renderWithQuery(InstancesPage);

		expect(await bodyRows()).toEqual([]);
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
