// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, type Run } from '#lib/testing/harness.js';
import PreviewPanel from './PreviewPanel.svelte';

const testInstanceId = '00000000-0000-0000-0000-000000000001';

const samplePreviewRun: Run = {
	id: '20000000-0000-0000-0000-000000000001',
	instance_id: testInstanceId,
	kind: 'missing',
	status: 'done',
	queued_at: '2026-09-01T12:00:00Z',
	started_at: '2026-09-01T12:00:01Z',
	finished_at: '2026-09-01T12:00:05Z',
	searched_count: 0,
	dry_run: true,
	seed: 42,
	previewed_targets: [
		{ entity_type: 'movie', id: 101, title: 'Inception', cost: 1 },
		{ entity_type: 'series', id: 202, title: 'Breaking Bad', cost: 3 }
	]
};

const sampleEmptyPreviewRun: Run = {
	id: '20000000-0000-0000-0000-000000000002',
	instance_id: testInstanceId,
	kind: 'missing',
	status: 'done',
	queued_at: '2026-09-01T12:00:00Z',
	started_at: '2026-09-01T12:00:01Z',
	finished_at: '2026-09-01T12:00:05Z',
	searched_count: 0,
	dry_run: true,
	seed: 99,
	previewed_targets: []
};

describe('PreviewPanel', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders targets, seed, and total cost sum on successful preview run', async () => {
		vi.spyOn(api, 'POST').mockImplementation(async (path: string) => {
			if (path === '/instances/{instanceId}/preview') {
				return {
					data: {
						run_id: samplePreviewRun.id,
						seed: 42
					}
				} as never;
			}
			throw new Error(`POST ${path} unexpected`);
		});

		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/runs/{runId}') {
				return { data: samplePreviewRun } as never;
			}
			throw new Error(`GET ${path} unexpected`);
		});

		renderWithQuery(PreviewPanel, { instanceId: testInstanceId });

		const missingBtn = screen.getByRole('button', { name: m.preview_missing() });
		await fireEvent.click(missingBtn);

		expect(api.POST).toHaveBeenCalledWith('/instances/{instanceId}/preview', {
			params: {
				path: { instanceId: testInstanceId },
				query: { kind: 'missing' }
			}
		});

		expect(await screen.findByText(m.preview_seed({ seed: 42 }))).toBeTruthy();

		const table = await screen.findByRole('table');
		expect(table).toBeTruthy();

		const rows = within(table).getAllByRole('row');
		expect(rows).toHaveLength(3);

		const firstTargetCells = within(rows[1] as HTMLElement).getAllByRole('cell');
		expect(firstTargetCells.map((c) => c.textContent?.trim())).toEqual([
			'Inception',
			'movie',
			'101',
			'1'
		]);

		const secondTargetCells = within(rows[2] as HTMLElement).getAllByRole('cell');
		expect(secondTargetCells.map((c) => c.textContent?.trim())).toEqual([
			'Breaking Bad',
			'series',
			'202',
			'3'
		]);

		expect(screen.getByText(m.preview_total_cost({ cost: 4 }))).toBeTruthy();
	});

	it('shows problem note when preview POST returns 409 conflict', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 409,
				detail: 'a run for kind upgrade is already active'
			})
		);

		renderWithQuery(PreviewPanel, { instanceId: testInstanceId });

		const upgradeBtn = screen.getByRole('button', { name: m.preview_upgrade() });
		await fireEvent.click(upgradeBtn);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('a run for kind upgrade is already active');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('shows empty state when previewed targets are empty', async () => {
		vi.spyOn(api, 'POST').mockResolvedValue({
			data: {
				run_id: sampleEmptyPreviewRun.id,
				seed: 99
			}
		} as never);

		vi.spyOn(api, 'GET').mockResolvedValue({
			data: sampleEmptyPreviewRun
		} as never);

		renderWithQuery(PreviewPanel, { instanceId: testInstanceId });

		const missingBtn = screen.getByRole('button', { name: m.preview_missing() });
		await fireEvent.click(missingBtn);

		expect(await screen.findByText(m.preview_seed({ seed: 99 }))).toBeTruthy();
		expect(await screen.findByText(m.preview_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});
});
