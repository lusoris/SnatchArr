// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	CSRF_HEADER,
	csrfMiddleware,
	describeError,
	expiryMiddleware,
	getCsrfToken,
	getExpiryHandler,
	problemStatus,
	setCsrfToken,
	setExpiryHandler
} from './client.js';

describe('csrfMiddleware', () => {
	beforeEach(() => setCsrfToken(''));

	it('signs unsafe requests only, and only once a token exists', async () => {
		const before = new Request('http://x/api/v1/instances', { method: 'POST' });
		await csrfMiddleware.onRequest?.({ request: before } as never);
		expect(before.headers.get(CSRF_HEADER)).toBeNull();

		setCsrfToken('tok');
		const get = new Request('http://x/api/v1/instances');
		const post = new Request('http://x/api/v1/instances', { method: 'POST' });
		await csrfMiddleware.onRequest?.({ request: get } as never);
		await csrfMiddleware.onRequest?.({ request: post } as never);
		expect(get.headers.get(CSRF_HEADER)).toBeNull();
		expect(post.headers.get(CSRF_HEADER)).toBe('tok');
	});

	it('harvests a fresh token from responses', async () => {
		setCsrfToken('stale');
		await csrfMiddleware.onResponse?.({ response: new Response(null) } as never);
		expect(getCsrfToken()).toBe('stale');
		const fresh = new Response(null, { headers: { [CSRF_HEADER]: 'fresh' } });
		await csrfMiddleware.onResponse?.({ response: fresh } as never);
		expect(getCsrfToken()).toBe('fresh');
	});
});

describe('describeError', () => {
	it('prefers the problem detail, then the title', () => {
		expect(
			describeError(new ProblemError({ type: 'about:blank', status: 502, detail: 'Seerr is down' }))
		).toBe('Seerr is down');
		expect(describeError(new ProblemError({ type: 'about:blank', title: 'Bad Gateway' }))).toBe(
			'Bad Gateway'
		);
		expect(describeError(new ProblemError({ type: 'about:blank' }))).toBe('request failed');
		expect(describeError(new Error('boom'))).toBe('boom');
		expect(describeError('plain')).toBe('plain');
	});
});

describe('problemStatus', () => {
	it('is the RFC 9457 status or 0', () => {
		expect(problemStatus(new ProblemError({ type: 'about:blank', status: 401 }))).toBe(401);
		expect(problemStatus(new ProblemError({ type: 'about:blank' }))).toBe(0);
		expect(problemStatus(new Error('x'))).toBe(0);
		expect(problemStatus(undefined)).toBe(0);
	});
});

describe('expiryMiddleware', () => {
	beforeEach(() => {
		setExpiryHandler(null);
	});

	it('registers and retrieves the expiry handler', () => {
		expect(getExpiryHandler()).toBeNull();
		const handler = vi.fn();
		setExpiryHandler(handler);
		expect(getExpiryHandler()).toBe(handler);
	});

	it('triggers the expiry handler once on 401 from a data endpoint', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const req = new Request('http://x/api/v1/hourly-caps');
		const res = new Response(null, { status: 401 });
		await expiryMiddleware.onResponse?.({ request: req, response: res } as never);
		expect(handler).toHaveBeenCalledTimes(1);
	});

	it('does not trigger the expiry handler on 401 from /auth/session or /auth/login', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const sessionReq = new Request('http://x/api/v1/auth/session');
		const loginReq = new Request('http://x/api/v1/auth/login');
		const res = new Response(null, { status: 401 });
		await expiryMiddleware.onResponse?.({ request: sessionReq, response: res } as never);
		await expiryMiddleware.onResponse?.({ request: loginReq, response: res } as never);
		expect(handler).not.toHaveBeenCalled();
	});

	it('asks the handler on 401 from /auth/password, which rechecks the session', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const req = new Request('http://x/api/v1/auth/password', { method: 'POST' });
		await expiryMiddleware.onResponse?.({
			request: req,
			response: new Response(null, { status: 401 })
		} as never);
		expect(handler).toHaveBeenCalledTimes(1);
	});

	it('does nothing on 200 OK', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const req = new Request('http://x/api/v1/hourly-caps');
		const res = new Response(null, { status: 200 });
		await expiryMiddleware.onResponse?.({ request: req, response: res } as never);
		expect(handler).not.toHaveBeenCalled();
	});

	it('deduplicates concurrent 401 responses', async () => {
		let resolveHandler = (): void => {};
		const handler = vi.fn(
			() =>
				new Promise<void>((resolve) => {
					resolveHandler = resolve;
				})
		);
		setExpiryHandler(handler);
		const req1 = new Request('http://x/api/v1/hourly-caps');
		const req2 = new Request('http://x/api/v1/prowlarr/status');
		const res = new Response(null, { status: 401 });
		const p1 = expiryMiddleware.onResponse?.({ request: req1, response: res } as never);
		const p2 = expiryMiddleware.onResponse?.({ request: req2, response: res } as never);
		resolveHandler();
		await Promise.all([p1, p2]);
		expect(handler).toHaveBeenCalledTimes(1);
	});

	it('does not trigger for schemaPath /auth/session or /auth/login', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const res = new Response(null, { status: 401 });
		await expiryMiddleware.onResponse?.({
			request: new Request('http://x/api/v1/session'),
			response: res,
			schemaPath: '/auth/session'
		} as never);
		await expiryMiddleware.onResponse?.({
			request: new Request('http://x/api/v1/login'),
			response: res,
			schemaPath: '/auth/login'
		} as never);
		expect(handler).not.toHaveBeenCalled();
	});

	it('does not trigger when already on a public route', async () => {
		const handler = vi.fn();
		setExpiryHandler(handler);
		const prevWindow = (globalThis as { window?: unknown }).window;
		(globalThis as { window?: unknown }).window = { location: { pathname: '/login' } };
		try {
			const req = new Request('http://x/api/v1/hourly-caps');
			const res = new Response(null, { status: 401 });
			await expiryMiddleware.onResponse?.({ request: req, response: res } as never);
			expect(handler).not.toHaveBeenCalled();
		} finally {
			(globalThis as { window?: unknown }).window = prevWindow;
		}
	});
});
