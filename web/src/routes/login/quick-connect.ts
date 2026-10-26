// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError, getClock } from '@sveltesentio/core';
import { createTimerOptions } from '#lib/activity.js';
import { api } from '#lib/api/client.js';

export const quickConnectConfig = {
	pollMs: 2000,
	maxAttempts: 30
};

export function waitPollInterval(ms: number, signal: AbortSignal): Promise<Date> {
	const { setTimeoutImpl, clearTimeoutImpl } = createTimerOptions();
	const clock = getClock();
	return new Promise((resolve) => {
		if (signal.aborted) {
			resolve(clock.now());
			return;
		}
		const onAbort = () => {
			clearTimeoutImpl(timerId);
			resolve(clock.now());
		};
		const timerId = setTimeoutImpl(() => {
			signal.removeEventListener('abort', onAbort);
			resolve(clock.now());
		}, ms);
		signal.addEventListener('abort', onAbort, { once: true });
	});
}

export async function pollQuickConnect(
	secret: string,
	signal: AbortSignal
): Promise<'authenticated' | 'timeout' | 'cancelled'> {
	const { pollMs, maxAttempts } = quickConnectConfig;
	for (let attempt = 0; attempt < maxAttempts; attempt++) {
		await waitPollInterval(pollMs, signal);
		if (signal.aborted) {
			return 'cancelled';
		}
		try {
			const res = await api.POST('/auth/jellyfin/quickconnect/authenticate', {
				body: { secret }
			});
			if (res.data) {
				return 'authenticated';
			}
		} catch (err) {
			if (err instanceof ProblemError && err.status === 409) {
				continue;
			}
			throw err;
		}
	}
	return 'timeout';
}
