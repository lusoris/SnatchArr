// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import CredentialFields from './CredentialFields.svelte';

describe('CredentialFields', () => {
	it('positive: renders username and password fields with error alert and passes axe audit', async () => {
		const { container } = render(CredentialFields, {
			usernameLabel: 'User Name',
			passwordLabel: 'Secret Password',
			username: 'alice',
			password: 'secretpassword',
			error: 'Invalid password'
		});

		const usernameInput = screen.getByLabelText('User Name') as HTMLInputElement;
		expect(usernameInput.id).toBe('username');
		expect(usernameInput.value).toBe('alice');
		expect(usernameInput.autocomplete).toBe('username');
		expect(usernameInput.required).toBe(true);

		const passwordInput = screen.getByLabelText('Secret Password') as HTMLInputElement;
		expect(passwordInput.id).toBe('password');
		expect(passwordInput.type).toBe('password');
		expect(passwordInput.value).toBe('secretpassword');
		expect(passwordInput.autocomplete).toBe('current-password');
		expect(passwordInput.required).toBe(true);

		const alert = screen.getByRole('alert');
		expect(alert.textContent).toBe('Invalid password');
		expect(alert.className).toContain('text-danger');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: omits error alert when error is empty and does not set minlength when unspecified', () => {
		render(CredentialFields, {
			usernameLabel: 'User Name',
			passwordLabel: 'Secret Password',
			error: ''
		});

		expect(screen.queryByRole('alert')).toBeNull();
		const usernameInput = screen.getByLabelText('User Name') as HTMLInputElement;
		expect(usernameInput.hasAttribute('minlength')).toBe(false);
	});

	it('boundary: accepts explicit ids, enforces minlength, and passes axe audit with clean state', async () => {
		const { container } = render(CredentialFields, {
			usernameId: 'jellyfin-username',
			passwordId: 'jellyfin-password',
			usernameLabel: 'Jellyfin User',
			passwordLabel: 'Jellyfin Password',
			usernameMinlength: 5,
			username: '',
			password: ''
		});

		const usernameInput = screen.getByLabelText('Jellyfin User') as HTMLInputElement;
		expect(usernameInput.id).toBe('jellyfin-username');
		expect(usernameInput.minLength).toBe(5);

		const passwordInput = screen.getByLabelText('Jellyfin Password') as HTMLInputElement;
		expect(passwordInput.id).toBe('jellyfin-password');

		await fireEvent.input(usernameInput, { target: { value: 'custom-user' } });
		expect(usernameInput.value).toBe('custom-user');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
