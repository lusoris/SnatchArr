// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { describeError } from './api/client.js';

export interface AuthSubmit {
	readonly busy: boolean;
	readonly error: string;
	submit(event: SubmitEvent): Promise<void>;
}

/**
 * Runes factory for auth submit handlers: manages busy/error state and navigation.
 *
 * @param action Async auth action to execute (e.g. login or setup).
 */
export function createAuthSubmit(action: () => Promise<unknown>): AuthSubmit {
	let busy = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await action();
			await goto(resolve('/'));
		} catch (err) {
			error = describeError(err);
		} finally {
			busy = false;
		}
	}

	return {
		get busy() {
			return busy;
		},
		get error() {
			return error;
		},
		submit
	};
}
