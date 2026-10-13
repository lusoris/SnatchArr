// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import LoginPage from './+page.svelte';

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

describe('login', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		navMocks.goto.mockReset();
	});

	it('renders heading, form fields and submit button', () => {
		renderWithQuery(LoginPage);

		expect(screen.getByRole('heading', { level: 1, name: m.login_title() })).toBeTruthy();
		expect(screen.getByLabelText(m.username())).toBeTruthy();
		expect(screen.getByLabelText(m.password())).toBeTruthy();
		expect(screen.getByRole('button', { name: m.login_submit() })).toBeTruthy();
	});

	it('submits credentials and navigates on success', async () => {
		stubGet({
			'/auth/setup/status': { setup_complete: true },
			'/auth/session': { user: { id: 'u1', username: 'admin', role: 'admin' } }
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(LoginPage);

		await fireEvent.input(screen.getByLabelText(m.username()), {
			target: { value: 'admin' }
		});
		await fireEvent.input(screen.getByLabelText(m.password()), {
			target: { value: 'secretpass' }
		});
		await fireEvent.click(screen.getByRole('button', { name: m.login_submit() }));

		expect(postSpy).toHaveBeenCalledWith('/auth/login', {
			body: { username: 'admin', password: 'secretpass' }
		});
		await waitFor(() => {
			expect(navMocks.goto).toHaveBeenCalledWith('/');
		});
	});

	it('displays error alert when login fails', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 401, detail: 'Invalid credentials' })
		);
		renderWithQuery(LoginPage);

		await fireEvent.input(screen.getByLabelText(m.username()), {
			target: { value: 'admin' }
		});
		await fireEvent.input(screen.getByLabelText(m.password()), {
			target: { value: 'wrongpass' }
		});
		await fireEvent.click(screen.getByRole('button', { name: m.login_submit() }));

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Invalid credentials');
		expect(navMocks.goto).not.toHaveBeenCalled();
		const submitButton = screen.getByRole('button', { name: m.login_submit() });
		expect((submitButton as HTMLButtonElement).disabled).toBe(false);
	});

	it('enforces required fields and blocks submission when empty', async () => {
		const postSpy = vi.spyOn(api, 'POST');
		renderWithQuery(LoginPage);

		const usernameInput = screen.getByLabelText(m.username()) as HTMLInputElement;
		const passwordInput = screen.getByLabelText(m.password()) as HTMLInputElement;

		expect(usernameInput.checkValidity()).toBe(false);
		expect(passwordInput.checkValidity()).toBe(false);
		expect(usernameInput.validationMessage).toBeTruthy();

		await fireEvent.click(screen.getByRole('button', { name: m.login_submit() }));
		expect(postSpy).not.toHaveBeenCalled();
	});
});
