// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createClient, type Middleware } from '@sveltesentio/api';
import { ProblemError } from '@sveltesentio/core';
import type { paths } from './schema.js';

/** Header the Go API expects on unsafe cookie-authenticated requests. */
export const CSRF_HEADER = 'X-CSRF-Token';

/** Routes accessible without an authenticated session. */
export const PUBLIC_ROUTES = new Set(['/login', '/setup']);

export function isPublicRoute(pathname: string): boolean {
	return PUBLIC_ROUTES.has(pathname);
}

let csrfToken = '';

/** Remembers the token the session endpoint handed out. */
export function setCsrfToken(token: string): void {
	csrfToken = token;
}

/** Current CSRF token (empty before the session was fetched). */
export function getCsrfToken(): string {
	return csrfToken;
}

export type ExpiryHandler = () => void | Promise<void>;

let expiryHandler: ExpiryHandler | null = null;

/** Registers the handler called when an API call reports an expired session (401). */
export function setExpiryHandler(handler: ExpiryHandler | null): void {
	expiryHandler = handler;
}

export function getExpiryHandler(): ExpiryHandler | null {
	return expiryHandler;
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

// Endpoints the session check itself calls, plus login: a 401 there is an answer, not an expiry.
const SESSION_PATHS = ['/auth/session', '/auth/setup/status', '/auth/login'];

function isSessionCheck(request: Request, schemaPath?: string): boolean {
	const path = schemaPath ?? new URL(request.url, 'http://localhost').pathname;
	return SESSION_PATHS.some((p) => path.endsWith(p));
}

function currentPath(): string {
	if (typeof window !== 'undefined' && window.location?.pathname) {
		return window.location.pathname;
	}
	return '/';
}

let expiring: Promise<void> | null = null;

async function triggerExpiry(): Promise<void> {
	if (!expiryHandler) return;
	if (expiring) return expiring;
	expiring = (async () => {
		try {
			await expiryHandler();
		} finally {
			expiring = null;
		}
	})();
	return expiring;
}

/** On a 401 from any other endpoint, asks the session handler to recheck the session once. */
export const expiryMiddleware: Middleware = {
	async onResponse({ request, response, schemaPath }) {
		if (
			response.status === 401 &&
			!isSessionCheck(request, schemaPath) &&
			!isPublicRoute(currentPath())
		) {
			await triggerExpiry();
		}
		return response;
	}
};

/** The typed client for /api/v1; throws ProblemError on RFC 9457 responses. */
export const api = createClient<paths>({
	baseUrl: '/api/v1',
	credentials: 'same-origin',
	middlewares: [csrfMiddleware, expiryMiddleware]
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
