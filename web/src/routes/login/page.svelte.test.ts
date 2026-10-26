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
import { quickConnectConfig } from './quick-connect.js';

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/login')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

async function submitLoginForm(password: string, username = 'admin'): Promise<void> {
	await fireEvent.input(screen.getByLabelText(m.username()), {
		target: { value: username }
	});
	await fireEvent.input(screen.getByLabelText(m.password()), {
		target: { value: password }
	});
	await fireEvent.click(screen.getByRole('button', { name: m.login_submit() }));
}

async function submitJellyfinForm(password: string, username = 'alice'): Promise<void> {
	await fireEvent.input(await screen.findByLabelText(m.login_jellyfin_username()), {
		target: { value: username }
	});
	await fireEvent.input(screen.getByLabelText(m.login_jellyfin_password()), {
		target: { value: password }
	});
	await fireEvent.click(screen.getByRole('button', { name: m.login_jellyfin_submit() }));
}

function stubReadySession(
	sso: Record<string, unknown> = { oidc: false, proxy: false, jellyfin: false }
) {
	return stubGet({
		'/auth/sso': sso,
		'/auth/setup/status': { setup_complete: true },
		'/auth/session': { user: { id: 'u1', username: 'admin', role: 'admin' } }
	});
}

describe('login', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		navMocks.goto.mockReset();
		pageMock.url = new URL('http://localhost/login');
		quickConnectConfig.pollMs = 2000;
		quickConnectConfig.maxAttempts = 30;
	});

	it('renders heading, form fields and submit button when jellyfin is disabled', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: false, jellyfinQuickConnect: false }
		});
		renderWithQuery(LoginPage);

		expect(screen.getByRole('heading', { level: 1, name: m.login_title() })).toBeTruthy();
		expect(screen.getByLabelText(m.username())).toBeTruthy();
		expect(screen.getByLabelText(m.password())).toBeTruthy();
		expect(screen.getByRole('button', { name: m.login_submit() })).toBeTruthy();
		expect(screen.queryByLabelText(m.login_jellyfin_username())).toBeNull();
		expect(screen.queryByText(m.login_admin_fallback())).toBeNull();
	});

	it('submits credentials and navigates on success', async () => {
		stubReadySession();
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(LoginPage);

		await submitLoginForm('secretpass');

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

		await submitLoginForm('wrongpass');

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

	it('shows Jellyfin primary form when jellyfin=true and hides local form until fallback disclosure is opened', async () => {
		stubReadySession({
			oidc: false,
			proxy: false,
			jellyfin: true,
			jellyfinQuickConnect: false
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(LoginPage);

		expect(await screen.findByLabelText(m.login_jellyfin_username())).toBeTruthy();
		expect(screen.getByLabelText(m.login_jellyfin_password())).toBeTruthy();
		expect(screen.getByRole('button', { name: m.login_jellyfin_submit() })).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.login_jellyfin_quick_connect() })).toBeNull();

		expect(screen.queryByLabelText(m.username())).toBeNull();
		expect(screen.queryByRole('button', { name: m.login_submit() })).toBeNull();

		const summary = screen.getByText(m.login_admin_fallback());
		await fireEvent.click(summary);

		expect(await screen.findByLabelText(m.username())).toBeTruthy();
		expect(screen.getByLabelText(m.password())).toBeTruthy();
		await submitLoginForm('admin-fallback-secret');

		expect(postSpy).toHaveBeenCalledWith('/auth/login', {
			body: { username: 'admin', password: 'admin-fallback-secret' }
		});
	});

	it('submits Jellyfin credentials to /auth/jellyfin and navigates on success', async () => {
		stubReadySession({
			oidc: false,
			proxy: false,
			jellyfin: true,
			jellyfinQuickConnect: false
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: { user: { id: 'u2', username: 'jf-alice', role: 'requester' } }
		} as never);
		renderWithQuery(LoginPage);

		await submitJellyfinForm('jelly-pass', 'alice');

		expect(postSpy).toHaveBeenCalledWith('/auth/jellyfin', {
			body: { username: 'alice', password: 'jelly-pass' }
		});
		await waitFor(() => {
			expect(navMocks.goto).toHaveBeenCalledWith('/');
		});
	});

	it('shows problem error alert when Jellyfin credentials fail', async () => {
		stubReadySession({
			oidc: false,
			proxy: false,
			jellyfin: true,
			jellyfinQuickConnect: false
		});
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 401,
				detail: 'Invalid Jellyfin credentials'
			})
		);
		renderWithQuery(LoginPage);

		await submitJellyfinForm('wrong-pass', 'alice');

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Invalid Jellyfin credentials');
		expect(navMocks.goto).not.toHaveBeenCalled();
	});

	it('initiates Quick Connect, shows code and polls authenticate until authorized', async () => {
		quickConnectConfig.pollMs = 5;
		quickConnectConfig.maxAttempts = 5;
		stubReadySession({
			oidc: false,
			proxy: false,
			jellyfin: true,
			jellyfinQuickConnect: true
		});
		let authCalls = 0;
		const postSpy = vi.spyOn(api, 'POST').mockImplementation(async (path: string) => {
			if (path === '/auth/jellyfin/quickconnect/initiate') {
				return { data: { code: '654321', secret: 'qc-secret-xyz' } } as never;
			}
			if (path === '/auth/jellyfin/quickconnect/authenticate') {
				authCalls++;
				if (authCalls === 1) {
					throw new ProblemError({
						type: 'about:blank',
						status: 409,
						detail: 'quick connect not yet authorized'
					});
				}
				return {
					data: { user: { id: 'u3', username: 'jf-carol', role: 'requester' } }
				} as never;
			}
			return { data: {} } as never;
		});
		renderWithQuery(LoginPage);

		const qcBtn = await screen.findByRole('button', { name: m.login_jellyfin_quick_connect() });
		await fireEvent.click(qcBtn);

		expect(
			await screen.findByText(m.login_jellyfin_quick_connect_code({ code: '654321' }))
		).toBeTruthy();

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/auth/jellyfin/quickconnect/authenticate', {
				body: { secret: 'qc-secret-xyz' }
			});
			expect(navMocks.goto).toHaveBeenCalledWith('/');
		});
		expect(authCalls).toBe(2);
	});
});
