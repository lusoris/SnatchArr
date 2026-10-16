// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import ConfirmAction from './ConfirmAction.svelte';

const props = { label: 'Forget', message: 'Forget it all?' };

describe('ConfirmAction', () => {
	it('leaves focus where it was until the step opens', () => {
		render(ConfirmAction, { ...props, onconfirm: vi.fn() });
		expect(document.activeElement).toBe(document.body);
	});

	it('moves focus to the safe choice when the step opens', async () => {
		render(ConfirmAction, { ...props, onconfirm: vi.fn() });
		await fireEvent.click(screen.getByRole('button', { name: 'Forget' }));
		await tick();
		expect(document.activeElement).toBe(screen.getByRole('button', { name: m.confirm_back() }));
	});

	it('returns focus to the trigger when the step is abandoned', async () => {
		const onconfirm = vi.fn();
		render(ConfirmAction, { ...props, onconfirm });
		await fireEvent.click(screen.getByRole('button', { name: 'Forget' }));
		await fireEvent.click(screen.getByRole('button', { name: m.confirm_back() }));
		await tick();
		expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Forget' }));
		expect(onconfirm).not.toHaveBeenCalled();
	});
});
