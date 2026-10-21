// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SetupPage from './+page.svelte';

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

async function clickSubmit(): Promise<void> {
	await fireEvent.click(screen.getByRole('button', { name: m.setup_submit() }));
}

async function submitSetup(secret?: string): Promise<void> {
	await fireEvent.input(screen.getByLabelText(m.password()), {
		target: { value: 'correct-horse-battery-staple' }
	});
	if (secret !== undefined) {
		await fireEvent.input(screen.getByLabelText(m.setup_secret()), {
			target: { value: secret }
		});
	}
	await clickSubmit();
}

describe('setup', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		navMocks.goto.mockReset();
	});

	it('renders heading, intro, username and password inputs, and submit button', () => {
		renderWithQuery(SetupPage);

		expect(screen.getByRole('heading', { level: 1, name: m.setup_title() })).toBeTruthy();
		expect(screen.getByText(m.setup_intro())).toBeTruthy();

		const usernameInput = screen.getByLabelText(m.username()) as HTMLInputElement;
		expect(usernameInput.value).toBe('admin');
		expect(screen.getByLabelText(m.password())).toBeTruthy();
		expect(screen.getByRole('button', { name: m.setup_submit() })).toBeTruthy();
	});

	it('submits initial admin setup and navigates to root', async () => {
		stubGet({
			'/auth/setup/status': { setup_complete: true },
			'/auth/session': { user: { id: 'u1', username: 'admin', role: 'admin' } }
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(SetupPage);

		await submitSetup();

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/auth/setup', {
				body: { username: 'admin', password: 'correct-horse-battery-staple' }
			});
		});
		await waitFor(() => {
			expect(navMocks.goto).toHaveBeenCalledWith('/');
		});
	});

	it('displays error alert when setup fails', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 400, detail: 'Password too weak' })
		);
		renderWithQuery(SetupPage);

		await submitSetup();

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Password too weak');
		expect(navMocks.goto).not.toHaveBeenCalled();
	});

	it('enforces minlength on password and blocks submission when empty', async () => {
		const postSpy = vi.spyOn(api, 'POST');
		renderWithQuery(SetupPage);

		const usernameInput = screen.getByLabelText(m.username()) as HTMLInputElement;
		const passwordInput = screen.getByLabelText(m.password()) as HTMLInputElement;
		expect(usernameInput.getAttribute('minlength')).toBe('3');
		expect(passwordInput.getAttribute('minlength')).toBe('12');
		expect(passwordInput.checkValidity()).toBe(false);
		expect(passwordInput.validity.valueMissing).toBe(true);

		await clickSubmit();
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('renders setup secret field when secret_required is true and submits with setup_secret', async () => {
		stubGet({
			'/auth/setup/status': { setup_complete: false, secret_required: true },
			'/auth/session': { user: null }
		});
		const { session } = await import('#lib/session.svelte.js');
		await session.refresh();
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(SetupPage);

		const secretInput = screen.getByLabelText(m.setup_secret()) as HTMLInputElement;
		expect(secretInput).toBeTruthy();
		await submitSetup('my-bootstrap-secret');

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/auth/setup', {
				body: {
					username: 'admin',
					password: 'correct-horse-battery-staple',
					setup_secret: 'my-bootstrap-secret'
				}
			});
		});
	});

	it('does not render setup secret field when secret_required is false', async () => {
		stubGet({
			'/auth/setup/status': { setup_complete: false, secret_required: false },
			'/auth/session': { user: null }
		});
		const { session } = await import('#lib/session.svelte.js');
		await session.refresh();
		renderWithQuery(SetupPage);

		expect(screen.queryByLabelText(m.setup_secret())).toBeNull();
	});
});
