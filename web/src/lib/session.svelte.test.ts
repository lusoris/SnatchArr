// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ GET: vi.fn(), POST: vi.fn() }));
vi.mock('./api/client.js', () => ({
	api: { GET: mocks.GET, POST: mocks.POST },
	problemStatus: (err: unknown) => (err as { status?: number } | undefined)?.status ?? 0
}));

import { session } from './session.svelte.js';

const admin = { id: 'u1', username: 'admin', role: 'admin' };

describe('session', () => {
	beforeEach(() => {
		mocks.GET.mockReset();
		mocks.POST.mockReset();
	});

	it('routes to setup while no user exists', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: false } });
		expect(await session.refresh()).toBe('setup');
		expect(session.user).toBeNull();
		expect(session.isAdmin).toBe(false);
	});

	it('is authenticated when the cookie session resolves a user', async () => {
		mocks.GET.mockResolvedValueOnce({ data: { setup_complete: true } }).mockResolvedValueOnce({
			data: { user: admin }
		});
		expect(await session.refresh()).toBe('authenticated');
		expect(session.isAdmin).toBe(true);
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
});
