// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery } from '#lib/testing/harness.js';
import ResetMemory from './ResetMemory.svelte';

const one = { instance: { id: forkInstance.id, name: forkInstance.name } };

function stubReset(forgotten = 7) {
	return vi.spyOn(api, 'POST').mockResolvedValue({ data: { forgotten } } as never);
}

describe('ResetMemory', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('resets one instance after a confirm that names it and both snatch kinds', async () => {
		const post = stubReset();
		renderWithQuery(ResetMemory, one);

		await fireEvent.click(screen.getByRole('button', { name: m.memory_reset_one() }));
		const ask = screen.getByRole('group', { name: m.memory_reset_one() });
		expect(ask.textContent).toContain(m.memory_reset_one_confirm({ name: 'Library Readarr' }));
		expect(ask.textContent).toContain('Library Readarr');
		expect(ask.textContent).toMatch(/missing and upgrade/);
		expect(post).not.toHaveBeenCalled();

		await fireEvent.click(screen.getByRole('button', { name: m.confirm_yes() }));

		expect(post).toHaveBeenCalledWith('/state/reset', { body: { instance_id: forkInstance.id } });
		expect(await screen.findByText(m.memory_reset_done({ count: 7 }))).toBeTruthy();
	});

	it('resets every instance after a confirm that says so', async () => {
		const post = stubReset(0);
		renderWithQuery(ResetMemory, {});

		await fireEvent.click(screen.getByRole('button', { name: m.memory_reset_all() }));
		const ask = screen.getByRole('group', { name: m.memory_reset_all() });
		expect(ask.textContent).toContain(m.memory_reset_all_confirm());
		expect(ask.textContent).toMatch(/every instance/);

		await fireEvent.click(screen.getByRole('button', { name: m.confirm_yes() }));

		expect(post).toHaveBeenCalledWith('/state/reset', { body: {} });
		expect(await screen.findByText(m.memory_reset_done({ count: 0 }))).toBeTruthy();
	});

	it('forgets nothing when the confirm step is abandoned', async () => {
		const post = stubReset();
		renderWithQuery(ResetMemory, one);

		await fireEvent.click(screen.getByRole('button', { name: m.memory_reset_one() }));
		await fireEvent.click(screen.getByRole('button', { name: m.confirm_back() }));

		expect(screen.queryByRole('group')).toBeNull();
		expect(screen.getByRole('button', { name: m.memory_reset_one() })).toBeTruthy();
		expect(post).not.toHaveBeenCalled();
	});

	it('announces the problem document when the reset is refused', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 403, detail: 'admin role required' })
		);
		renderWithQuery(ResetMemory, {});

		await fireEvent.click(screen.getByRole('button', { name: m.memory_reset_all() }));
		await fireEvent.click(screen.getByRole('button', { name: m.confirm_yes() }));

		expect((await screen.findByRole('alert')).textContent).toContain('admin role required');
	});
});
