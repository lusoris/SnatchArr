// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createClient, type Middleware } from '@sveltesentio/api';
import { ProblemError } from '@sveltesentio/core';
import type { paths } from './schema.js';

/** Header the Go API expects on unsafe cookie-authenticated requests. */
export const CSRF_HEADER = 'X-CSRF-Token';

let csrfToken = '';

/** Remembers the token the session endpoint handed out. */
export function setCsrfToken(token: string): void {
	csrfToken = token;
}

/** Current CSRF token (empty before the session was fetched). */
export function getCsrfToken(): string {
	return csrfToken;
}

const SAFE = new Set(['GET', 'HEAD', 'OPTIONS']);

/** Attaches the CSRF token to unsafe requests and harvests it from responses. */
export const csrfMiddleware: Middleware = {
	onRequest({ request }) {
		if (!SAFE.has(request.method) && csrfToken) request.headers.set(CSRF_HEADER, csrfToken);
		return request;
	},
	onResponse({ response }) {
		const fresh = response.headers.get(CSRF_HEADER);
		if (fresh) csrfToken = fresh;
		return response;
	}
};

/** The typed client for /api/v1; throws ProblemError on RFC 9457 responses. */
export const api = createClient<paths>({
	baseUrl: '/api/v1',
	credentials: 'same-origin',
	middlewares: [csrfMiddleware]
});

/** Human text for any thrown error, preferring the problem detail. */
export function describeError(err: unknown): string {
	if (err instanceof ProblemError) return err.detail ?? err.title ?? 'request failed';
	if (err instanceof Error) return err.message;
	return String(err);
}

/** Status code of a thrown ProblemError, or 0 for anything else. */
export function problemStatus(err: unknown): number {
	return err instanceof ProblemError ? (err.status ?? 0) : 0;
}
