// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, sampleRuns, stubGet } from '#lib/testing/harness.js';
import RunList from './RunList.svelte';

/** Renders the run list of the fork instance and returns its body rows. */
async function rows(runs: unknown = sampleRuns): Promise<HTMLElement[]> {
	stubGet({ '/runs': runs });
	renderWithQuery(RunList, { instanceId: forkInstance.id });
	return within(await screen.findByRole('table'))
		.getAllByRole('row')
		.slice(1);
}

function cells(row: HTMLElement): string[] {
	return within(row)
		.getAllByRole('cell')
		.map((c) => c.textContent?.trim() ?? '');
}

describe('RunList', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('lists the runs of the instance with kind, status, end and searched count', async () => {
		const body = await rows();

		expect(api.GET).toHaveBeenCalledWith('/runs', {
			params: { query: { instance_id: forkInstance.id, limit: 20 } }
		});
		expect(body.map((r) => cells(r).slice(0, 2))).toEqual([
			[m.snatch_kind_missing(), m.run_status_queued()],
			[m.snatch_kind_upgrade(), m.run_status_leased()],
			[m.snatch_kind_missing(), m.run_status_done()]
		]);
		expect(body.map((r) => cells(r)[4])).toEqual(['0', '3', '12']);
		expect(cells(body[1] as HTMLElement)[3]).toBe('–');
		expect(cells(body[2] as HTMLElement)[3]).not.toBe('–');
		expect(cells(body[2] as HTMLElement)[2]).not.toBe('');
	});

	it('offers cancel only for queued and leased runs', async () => {
		const body = await rows();

		const offered = body.map(
			(r) => within(r).queryByRole('button', { name: m.run_cancel() }) !== null
		);
		expect(offered).toEqual([true, true, false]);
	});

	it('says so when the instance has no run', async () => {
		stubGet({ '/runs': [] });
		renderWithQuery(RunList, { instanceId: forkInstance.id });

		expect(await screen.findByText(m.runs_empty())).toBeTruthy();
	});

	it('cancels a run only after the confirm step', async () => {
		const post = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		const body = await rows();

		await fireEvent.click(
			within(body[1] as HTMLElement).getByRole('button', { name: m.run_cancel() })
		);
		const ask = screen.getByRole('group', { name: m.run_cancel() });
		expect(ask.textContent).toContain(m.run_cancel_confirm({ kind: m.snatch_kind_upgrade() }));
		expect(post).not.toHaveBeenCalled();

		await fireEvent.click(within(ask).getByRole('button', { name: m.confirm_yes() }));
		expect(post).toHaveBeenCalledWith('/runs/{runId}/cancel', {
			params: { path: { runId: sampleRuns[1]?.id } }
		});
	});

	it('leaves the run alone when the confirm step is abandoned', async () => {
		const post = vi.spyOn(api, 'POST');
		const body = await rows();

		await fireEvent.click(
			within(body[0] as HTMLElement).getByRole('button', { name: m.run_cancel() })
		);
		await fireEvent.click(screen.getByRole('button', { name: m.confirm_back() }));

		expect(screen.queryByRole('group')).toBeNull();
		expect(
			within(body[0] as HTMLElement).getByRole('button', { name: m.run_cancel() })
		).toBeTruthy();
		expect(post).not.toHaveBeenCalled();
	});

	it('announces the problem document when the cancel is refused', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 409, detail: 'run already finished' })
		);
		const body = await rows();

		await fireEvent.click(
			within(body[0] as HTMLElement).getByRole('button', { name: m.run_cancel() })
		);
		await fireEvent.click(screen.getByRole('button', { name: m.confirm_yes() }));

		expect((await screen.findByRole('alert')).textContent).toContain('run already finished');
	});

	it('announces the problem document when the run list is refused', async () => {
		stubGet({
			'/runs': new ProblemError({ type: 'about:blank', status: 503, detail: 'store down' })
		});
		renderWithQuery(RunList, { instanceId: forkInstance.id });

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('table')).toBeNull();
	});
});
