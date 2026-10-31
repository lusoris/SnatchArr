// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ProblemError } from '@sveltesentio/core';

const nav = vi.hoisted(() => ({
	goto: vi.fn(async (route?: string) => {
		void route;
	})
}));

vi.mock('$app/navigation', () => ({
	goto: (route: string) => nav.goto(route)
}));

vi.mock('$app/paths', () => ({
	resolve: (route: string) => (route.startsWith('/') ? route : `/${route}`)
}));

import { createAuthSubmit } from './auth-submit.svelte.js';

function createMockSubmitEvent(): { event: SubmitEvent; preventDefault: ReturnType<typeof vi.fn> } {
	const preventDefault = vi.fn();
	const event = {
		preventDefault
	} as unknown as SubmitEvent;
	return { event, preventDefault };
}

describe('createAuthSubmit', () => {
	beforeEach(() => {
		nav.goto.mockReset();
	});

	it('positive: prevents default, toggles busy, invokes action and navigates to root on success', async () => {
		let actionCalled = false;
		let busyDuringAction = false;

		const auth = createAuthSubmit(async () => {
			actionCalled = true;
			busyDuringAction = auth.busy;
		});

		expect(auth.busy).toBe(false);
		expect(auth.error).toBe('');

		const { event, preventDefault } = createMockSubmitEvent();
		await auth.submit(event);

		expect(preventDefault).toHaveBeenCalledTimes(1);
		expect(actionCalled).toBe(true);
		expect(busyDuringAction).toBe(true);
		expect(auth.busy).toBe(false);
		expect(auth.error).toBe('');
		expect(nav.goto).toHaveBeenCalledWith('/');
	});

	it('negative: catches error, converts to message, does not navigate, and resets busy', async () => {
		const auth = createAuthSubmit(async () => {
			throw new ProblemError({ type: 'about:blank', status: 400, detail: 'Invalid credentials' });
		});

		const { event } = createMockSubmitEvent();
		await auth.submit(event);

		expect(auth.error).toBe('Invalid credentials');
		expect(auth.busy).toBe(false);
		expect(nav.goto).not.toHaveBeenCalled();
	});

	it('boundary: clears previous error on re-submit', async () => {
		let succeed = false;
		const auth = createAuthSubmit(async () => {
			if (!succeed) {
				throw new Error('First try failed');
			}
		});

		const { event } = createMockSubmitEvent();
		await auth.submit(event);
		expect(auth.error).toBe('First try failed');

		succeed = true;
		await auth.submit(event);
		expect(auth.error).toBe('');
		expect(auth.busy).toBe(false);
		expect(nav.goto).toHaveBeenCalledWith('/');
	});
});
