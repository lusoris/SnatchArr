// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import ConfirmDelete from './ConfirmDelete.svelte';

describe('ConfirmDelete', () => {
	it('asks in the page and confirms only on the explicit answer', async () => {
		const onconfirm = vi.fn();
		const { container } = render(ConfirmDelete, { name: 'qBit', onconfirm });

		await fireEvent.click(screen.getByRole('button', { name: m.action_delete() }));
		expect(onconfirm).not.toHaveBeenCalled();
		expect(screen.getByRole('group', { name: m.confirm_delete({ name: 'qBit' }) })).toBeTruthy();
		expect(document.activeElement).toBe(screen.getByRole('button', { name: m.action_cancel() }));
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);

		await fireEvent.click(screen.getByRole('button', { name: m.confirm_delete_yes() }));
		expect(onconfirm).toHaveBeenCalledTimes(1);
	});

	it('goes back to the button, focused, when cancelled', async () => {
		const onconfirm = vi.fn();
		render(ConfirmDelete, { name: 'qBit', onconfirm });

		await fireEvent.click(screen.getByRole('button', { name: m.action_delete() }));
		await fireEvent.click(screen.getByRole('button', { name: m.action_cancel() }));

		expect(onconfirm).not.toHaveBeenCalled();
		expect(screen.queryByRole('group')).toBeNull();
		expect(document.activeElement).toBe(screen.getByRole('button', { name: m.action_delete() }));
	});

	it('cannot ask while disabled', () => {
		render(ConfirmDelete, { name: 'qBit', disabled: true, onconfirm: vi.fn() });
		expect(
			(screen.getByRole('button', { name: m.action_delete() }) as HTMLButtonElement).disabled
		).toBe(true);
	});
});
