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

		await fireEvent.input(screen.getByLabelText(m.password()), {
			target: { value: 'correct-horse-battery-staple' }
		});
		await fireEvent.click(screen.getByRole('button', { name: m.setup_submit() }));

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

		await fireEvent.input(screen.getByLabelText(m.password()), {
			target: { value: 'correct-horse-battery-staple' }
		});
		await fireEvent.click(screen.getByRole('button', { name: m.setup_submit() }));

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

		await fireEvent.click(screen.getByRole('button', { name: m.setup_submit() }));
		expect(postSpy).not.toHaveBeenCalled();
	});
});
