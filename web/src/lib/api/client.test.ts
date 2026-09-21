// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { beforeEach, describe, expect, it } from 'vitest';
import {
	CSRF_HEADER,
	csrfMiddleware,
	describeError,
	getCsrfToken,
	problemStatus,
	setCsrfToken
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
