// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import AdminSaveButton from './AdminSaveButton.svelte';

describe('AdminSaveButton', () => {
	afterEach(() => {
		session.user = null;
	});

	it('positive: renders save button for admin user and passes WCAG', async () => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		const { container } = render(AdminSaveButton);

		const btn = screen.getByRole('button', { name: m.instance_save() });
		expect(btn).toBeTruthy();
		expect(btn.getAttribute('type')).toBe('submit');
		expect((btn as HTMLButtonElement).disabled).toBe(false);

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: does not render button when user is not admin', () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		render(AdminSaveButton);

		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('boundary: disables button when disabled prop is true', () => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		render(AdminSaveButton, { props: { disabled: true } });

		const btn = screen.getByRole('button', { name: m.instance_save() }) as HTMLButtonElement;
		expect(btn.disabled).toBe(true);
	});
});
