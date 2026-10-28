// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { runStatusLabel, snatchKindLabel } from '#lib/runs.js';
import { formatWhen } from '#lib/stamina.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import { renderWithQuery, sampleRuns, stubGet } from '#lib/testing/harness.js';
import RunDialog from './RunDialog.svelte';

const run = sampleRuns[2];

describe('RunDialog', () => {
	afterEach(() => vi.restoreAllMocks());

	it('fetches and renders run fields and instance href', async () => {
		const get = stubGet({ '/runs/{runId}': run });
		renderWithQuery(RunDialog, { runId: run.id, open: true });
		await screen.findByText(runStatusLabel(run.status));
		const dialog = within(screen.getByRole('dialog'));
		expect(dialog.getByText(snatchKindLabel(run.kind))).toBeTruthy();
		expect(dialog.getByText(String(run.searched_count))).toBeTruthy();
		for (const ts of [run.queued_at, run.started_at, run.finished_at]) {
			expect(dialog.getByText(formatWhen(ts, getLocale()))).toBeTruthy();
		}
		expect(dialog.getByRole('link').getAttribute('href')).toBe(`/instances/${run.instance_id}`);
		expect(get).toHaveBeenCalledWith('/runs/{runId}', { params: { path: { runId: run.id } } });
	});

	it('announces an RFC 9457 problem', async () => {
		stubGet({
			'/runs/{runId}': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'Run service unavailable'
			})
		});
		renderWithQuery(RunDialog, { runId: run.id, open: true });
		expect((await screen.findByRole('alert')).textContent).toContain('Run service unavailable');
	});
});

describe('RunDialog lifecycle', () => {
	afterEach(() => vi.restoreAllMocks());

	it('shows loading, zero count and absent timestamps; closes on Escape', async () => {
		let answer!: (value: never) => void;
		vi.spyOn(api, 'GET').mockImplementation(
			() =>
				new Promise((resolve) => {
					answer = resolve;
				})
		);
		renderWithQuery(RunDialog, { runId: run.id, open: true });
		expect(screen.getByText(m.loading())).toBeTruthy();
		answer({ data: { ...sampleRuns[0] } } as never);
		expect(await screen.findByText('0')).toBeTruthy();
		expect(screen.getAllByText('–')).toHaveLength(2);
		await fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
		expect(screen.queryByRole('dialog')).toBeNull();
	});

	it('does not fetch while closed', () => {
		const get = stubGet({});
		renderWithQuery(RunDialog, { runId: run.id, open: false });
		expect(get).not.toHaveBeenCalled();
		expect(screen.queryByRole('dialog')).toBeNull();
	});
});

describe('RunDialog selection', () => {
	afterEach(() => vi.restoreAllMocks());
	it('fetches the newly selected run when its id changes', async () => {
		const next = sampleRuns[1];
		const get = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce({ data: run } as never)
			.mockResolvedValueOnce({ data: next } as never);
		const view = renderWithQuery(RunDialog, { runId: run.id, open: true });
		expect(await screen.findByText(runStatusLabel(run.status))).toBeTruthy();
		await view.rerender({ runId: next.id, open: true });
		expect(await screen.findByText(runStatusLabel(next.status))).toBeTruthy();
		expect(get).toHaveBeenLastCalledWith('/runs/{runId}', { params: { path: { runId: next.id } } });
	});
});
