// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { api, problemStatus } from './api/client.js';
import type { components } from './api/schema.js';

export type User = components['schemas']['User'];

/** What the layout needs to route: nobody yet, logged out, or logged in. */
export type SessionState = 'unknown' | 'setup' | 'anonymous' | 'authenticated';

class Session {
	state = $state<SessionState>('unknown');
	user = $state<User | null>(null);
	error = $state<string>('');

	get isAdmin(): boolean {
		return this.user?.role === 'admin';
	}

	/** Resolves the state from the API: setup status first, then the cookie session. */
	async refresh(): Promise<SessionState> {
		try {
			const setup = await api.GET('/auth/setup/status');
			if (setup.data && !setup.data.setup_complete) {
				this.state = 'setup';
				this.user = null;
				return this.state;
			}
			const me = await api.GET('/auth/session');
			this.user = me.data?.user ?? null;
			this.state = this.user ? 'authenticated' : 'anonymous';
		} catch (err) {
			this.user = null;
			this.state = 'anonymous';
			this.error = problemStatus(err) === 401 ? '' : String(err);
		}
		return this.state;
	}

	async login(username: string, password: string): Promise<void> {
		await api.POST('/auth/login', { body: { username, password } });
		await this.refresh();
	}

	async setup(username: string, password: string): Promise<void> {
		await api.POST('/auth/setup', { body: { username, password } });
		await this.login(username, password);
	}

	async logout(): Promise<void> {
		try {
			await api.POST('/auth/logout');
		} finally {
			this.user = null;
			this.state = 'anonymous';
		}
	}
}

/** The one session store the whole SPA shares. */
export const session = new Session();
