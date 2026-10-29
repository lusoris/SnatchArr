// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it, vi } from 'vitest';
import AutosaveCheckbox from './AutosaveCheckbox.svelte';

const base = { id: 'fav', label: 'Synchronize favourites' };

describe('AutosaveCheckbox', () => {
	it('saves on change and says so', async () => {
		const save = vi.fn().mockResolvedValue(undefined);
		const { container } = render(AutosaveCheckbox, { ...base, checked: true, save });
		const box = screen.getByRole('checkbox', { name: base.label }) as HTMLInputElement;
		await fireEvent.click(box);
		expect(save).toHaveBeenCalledWith(false);
		await waitFor(() =>
			expect(screen.getByRole('status').getAttribute('data-status')).toBe('saved')
		);
		expect(box.checked).toBe(false);
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('puts the box back and names the failure when the save is refused', async () => {
		const save = vi
			.fn()
			.mockRejectedValue(
				new ProblemError({ type: 'about:blank', status: 500, detail: 'store down' })
			);
		render(AutosaveCheckbox, { ...base, checked: false, save });
		const box = screen.getByRole('checkbox', { name: base.label }) as HTMLInputElement;
		await fireEvent.click(box);
		await waitFor(() =>
			expect(screen.getByRole('status').getAttribute('data-status')).toBe('failed')
		);
		expect(screen.getByRole('status').textContent).toContain('store down');
		expect(box.checked).toBe(false);
	});

	it('locks the box while a save is in flight', async () => {
		let finish: (stored: boolean | undefined) => void = () => {};
		const save = vi.fn(() => new Promise<boolean | undefined>((resolve) => (finish = resolve)));
		render(AutosaveCheckbox, { ...base, checked: false, save });
		const box = screen.getByRole('checkbox', { name: base.label }) as HTMLInputElement;
		await fireEvent.click(box);
		await waitFor(() => expect(box.disabled).toBe(true));
		expect(screen.getByRole('status').getAttribute('data-status')).toBe('saving');
		finish(undefined);
		await waitFor(() => expect(box.disabled).toBe(false));
		expect(save).toHaveBeenCalledTimes(1);
	});

	it('shows the value the server stored, then follows a newer one', async () => {
		const save = vi.fn().mockResolvedValue(true);
		const { rerender } = render(AutosaveCheckbox, { ...base, checked: true, save });
		const box = screen.getByRole('checkbox', { name: base.label }) as HTMLInputElement;
		await fireEvent.click(box);
		await waitFor(() =>
			expect(screen.getByRole('status').getAttribute('data-status')).toBe('saved')
		);
		expect(box.checked).toBe(true);
		await rerender({ checked: false });
		await waitFor(() => expect(box.checked).toBe(false));
	});

	it('shows no status before the first change', () => {
		render(AutosaveCheckbox, { ...base, checked: true, save: vi.fn() });
		expect(screen.getByRole('status').getAttribute('data-status')).toBe('idle');
		expect(screen.getByRole('status').textContent?.trim()).toBe('');
	});
});
