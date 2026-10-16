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

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/login')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

describe('login', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		navMocks.goto.mockReset();
		pageMock.url = new URL('http://localhost/login');
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

	it('renders SSO button when oidc is enabled', async () => {
		stubGet({
			'/auth/sso': { oidc: true, proxy: false }
		});
		renderWithQuery(LoginPage);

		const ssoLink = await screen.findByRole('link', { name: m.login_sso() });
		expect(ssoLink).toBeTruthy();
		expect(ssoLink.getAttribute('href')).toBe('/api/v1/auth/oidc/login');
	});

	it('does not render SSO button when oidc is disabled', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false }
		});
		renderWithQuery(LoginPage);

		await waitFor(() => {
			expect(screen.queryByRole('link', { name: m.login_sso() })).toBeNull();
		});
	});

	it('displays SSO failed error alert when sso query param is failed', () => {
		pageMock.url = new URL('http://localhost/login?sso=failed');
		renderWithQuery(LoginPage);

		const alert = screen.getByRole('alert');
		expect(alert.textContent).toContain(m.login_sso_failed());
	});

	it('attempts proxy login and navigates when proxy is enabled and successful', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true },
			'/auth/session': { user: { id: 'u1', username: 'admin', role: 'admin' } }
		});
		const postSpy = vi.spyOn(api, 'POST').mockImplementation(async (path: string) => {
			if (path === '/auth/proxy-session') {
				return { data: { user: { id: 'u1', username: 'admin', role: 'admin' } } } as never;
			}
			return { data: {} } as never;
		});

		renderWithQuery(LoginPage);

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/auth/proxy-session');
			expect(navMocks.goto).toHaveBeenCalledWith('/');
		});
	});

	it('handles proxy login failure gracefully and remains on login form', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true }
		});
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 401, detail: 'Proxy auth failed' })
		);

		renderWithQuery(LoginPage);

		await waitFor(() => {
			expect(screen.getByRole('button', { name: m.login_submit() })).toBeTruthy();
		});
		expect(navMocks.goto).not.toHaveBeenCalled();
	});
});
