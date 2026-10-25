// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import NewEventsPill from './NewEventsPill.svelte';

describe('NewEventsPill', () => {
	it('positive: renders count n and clicking triggers onsnap callback', async () => {
		const onsnap = vi.fn();
		render(NewEventsPill, {
			props: { count: 7, onsnap }
		});

		const label = m.activity_new_events({ count: 7 });
		const btn = screen.getByRole('button', { name: label });
		expect(btn).toBeTruthy();

		await fireEvent.click(btn);
		expect(onsnap).toHaveBeenCalledOnce();
	});

	it('negative: does not render button when count is 0', () => {
		const onsnap = vi.fn();
		render(NewEventsPill, {
			props: { count: 0, onsnap }
		});

		expect(screen.queryByRole('button')).toBeNull();
	});

	it('boundary: renders for count 1 and hides for negative count', () => {
		const onsnap = vi.fn();
		const { unmount } = render(NewEventsPill, {
			props: { count: 1, onsnap }
		});

		expect(screen.getByRole('button', { name: m.activity_new_events({ count: 1 }) })).toBeTruthy();
		unmount();

		render(NewEventsPill, {
			props: { count: -5, onsnap }
		});
		expect(screen.queryByRole('button')).toBeNull();
	});
});
