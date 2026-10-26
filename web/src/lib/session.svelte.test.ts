// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
	GET: vi.fn(),
	POST: vi.fn(),
	setExpiryHandler: vi.fn(),
	goto: vi.fn()
}));
vi.mock('./api/client.js', () => ({
	api: { GET: mocks.GET, POST: mocks.POST },
	problemStatus: (err: unknown) => (err as { status?: number } | undefined)?.status ?? 0,
	setExpiryHandler: mocks.setExpiryHandler
}));
vi.mock('$app/navigation', () => ({
	goto: mocks.goto
}));
vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}));

import { session } from './session.svelte.js';

const admin = { id: 'u1', username: 'admin', role: 'admin' };
const requester = { id: 'u2', username: 'requester', role: 'requester' };
const viewer = { id: 'u3', username: 'viewer', role: 'viewer' };

describe('session', () => {
	beforeEach(() => {
		mocks.GET.mockReset();
		mocks.POST.mockReset();
		mocks.goto.mockReset();
		session.user = null;
		session.state = 'unknown';
		session.error = '';
	});

	it('routes to setup while no user exists', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: false } });
		expect(await session.refresh()).toBe('setup');
		expect(session.user).toBeNull();
		expect(session.isAdmin).toBe(false);
		expect(session.isRequester).toBe(false);
	});

	it('is authenticated when the cookie session resolves a user', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: true } }).mockResolvedValueOnce({
			data: { user: admin }
		});
		expect(await session.refresh()).toBe('authenticated');
		expect(session.isAdmin).toBe(true);
		expect(session.isRequester).toBe(true);
	});

	it('positive: isRequester is true for admin and requester', () => {
		session.user = admin as never;
		expect(session.isRequester).toBe(true);

		session.user = requester as never;
		expect(session.isRequester).toBe(true);
	});

	it('negative: isRequester is false for viewer', () => {
		session.user = viewer as never;
		expect(session.isRequester).toBe(false);
	});

	it('boundary: isRequester is false when no user is logged in', () => {
		session.user = null;
		expect(session.isRequester).toBe(false);
	});

	it('treats 401 as anonymous without an error', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: true } }).mockRejectedValueOnce({
			status: 401
		});
		expect(await session.refresh()).toBe('anonymous');
		expect(session.error).toBe('');
	});

	it('keeps other failures visible', async () => {
		mocks.GET.mockRejectedValueOnce(new Error('boom'));
		expect(await session.refresh()).toBe('anonymous');
		expect(session.error).toContain('boom');
	});

	it('sets up, logs in and logs out through the API', async () => {
		mocks.POST.mockResolvedValue({ data: {} });
		mocks.GET.mockResolvedValue({ data: { setup_complete: true, user: admin } });
		await session.setup('admin', 'correct horse battery staple');
		expect(mocks.POST).toHaveBeenCalledWith('/auth/setup', {
			body: { username: 'admin', password: 'correct horse battery staple' }
		});
		expect(mocks.POST).toHaveBeenCalledWith('/auth/login', {
			body: { username: 'admin', password: 'correct horse battery staple' }
		});
		expect(session.state).toBe('authenticated');

		mocks.POST.mockRejectedValueOnce(new Error('offline'));
		await expect(session.logout()).rejects.toThrow('offline');
		expect(session.state).toBe('anonymous');
		expect(session.user).toBeNull();
	});

	it('expires the session and routes to login', async () => {
		session.state = 'authenticated';
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: true } }).mockRejectedValueOnce({
			status: 401
		});
		await session.expire();
		expect(session.state).toBe('anonymous');
		expect(session.user).toBeNull();
		expect(mocks.goto).toHaveBeenCalledWith('login');
	});

	it('keeps a live session when a 401 was a wrong credential, not an expiry', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: true } }).mockResolvedValueOnce({
			data: { user: admin }
		});
		await session.expire();
		expect(session.state).toBe('authenticated');
		expect(session.user).toEqual(admin);
		expect(mocks.goto).not.toHaveBeenCalled();
	});
});
