// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import NewPasswordField from './NewPasswordField.svelte';

function newPasswordInput(): HTMLInputElement {
	return screen.getByLabelText(m.new_password()) as HTMLInputElement;
}

describe('NewPasswordField', () => {
	it('positive: renders a required password input with the 12-character hint', async () => {
		const { container } = render(NewPasswordField, { value: '' });
		const input = newPasswordInput();

		expect(input.id).toBe('new-password');
		expect(input.type).toBe('password');
		expect(input.autocomplete).toBe('new-password');
		expect(input.required).toBe(true);
		expect(input.minLength).toBe(12);
		expect(input.getAttribute('aria-describedby')).toBe('new-password-hint');
		expect(screen.getByText(m.password_min_hint())).toBeTruthy();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: never marks the input invalid on its own', () => {
		render(NewPasswordField, { value: 'short' });
		const input = newPasswordInput();

		expect(input.getAttribute('aria-invalid')).toBeNull();
		expect(input.getAttribute('aria-describedby')).toBe('new-password-hint');
	});

	it('boundary: shows the initial value and follows typed input', async () => {
		render(NewPasswordField, { value: 'prefilled-pass-12' });
		const input = newPasswordInput();
		expect(input.value).toBe('prefilled-pass-12');

		await fireEvent.input(input, { target: { value: '' } });
		expect(input.value).toBe('');
	});
});
