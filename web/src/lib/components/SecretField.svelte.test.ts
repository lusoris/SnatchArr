// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import SecretField from './SecretField.svelte';

function secretInput(): HTMLInputElement {
	return screen.getByLabelText('API key') as HTMLInputElement;
}

describe('SecretField', () => {
	it('is a required, empty password field on create', async () => {
		const { container } = render(SecretField, {
			id: 'key',
			label: 'API key',
			value: '',
			editing: false,
			requiredOnCreate: true
		});
		expect(secretInput().type).toBe('password');
		expect(secretInput().value).toBe('');
		expect(secretInput().required).toBe(true);
		expect(secretInput().getAttribute('aria-describedby')).toBe('key-hint');
		expect(screen.getByText(m.secret_new_hint())).toBeTruthy();
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('is optional while editing, and says that empty keeps the stored value', () => {
		render(SecretField, {
			id: 'key',
			label: 'API key',
			value: '',
			editing: true,
			requiredOnCreate: true
		});
		expect(secretInput().required).toBe(false);
		expect(screen.getByText(m.secret_keep_hint())).toBeTruthy();
	});

	it('ties a field-level problem to the input', async () => {
		const { container } = render(SecretField, {
			id: 'key',
			label: 'API key',
			value: '',
			editing: false,
			error: 'too short'
		});
		expect(secretInput().getAttribute('aria-invalid')).toBe('true');
		expect(secretInput().getAttribute('aria-describedby')).toBe('key-hint key-error');
		expect(screen.getByText('too short').id).toBe('key-error');
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
