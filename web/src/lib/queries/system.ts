// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createSentioQuery } from '@sveltesentio/query';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { keys } from '#lib/query.js';
import { session } from '#lib/session.svelte.js';

export type SystemStatus = components['schemas']['SystemStatus'];
export type ModuleState = components['schemas']['ModuleState'];

/**
 * Shared query helper for system status.
 * Reuses keys.systemStatus and typed API client GET /system/status.
 */
export function systemStatusQuery() {
	return createSentioQuery<SystemStatus | undefined>({
		queryKey: keys.systemStatus,
		queryFn: async () => (await api.GET('/system/status')).data
	});
}

/**
 * Shared query helper for optional modules.
 * Reuses keys.modules and typed API client GET /system/modules; by default it fetches only while
 * authenticated. Passing `requireSession: false` fetches in every session state, so a caller that
 * renders the pending state shows the API problem instead of waiting for a session that never comes.
 */
export function systemModulesQuery({ requireSession = true }: { requireSession?: boolean } = {}) {
	return createSentioQuery<ModuleState[]>(() => ({
		queryKey: keys.modules,
		enabled: !requireSession || session.state === 'authenticated',
		queryFn: async () => (await api.GET('/system/modules')).data ?? []
	}));
}

/**
 * Formats a version and commit into the shell badge label.
 * Returns empty string when version is missing.
 * In dev builds (e.g. version="dev", empty commit) renders as-is without commit.
 */
export function formatVersionLabel(version?: string | null, commit?: string | null): string {
	if (!version) return '';
	const cleanCommit = commit?.trim() ?? '';
	const shortCommit = cleanCommit.slice(0, 7);
	const prefix = version === 'dev' || version.startsWith('v') ? version : `v${version}`;
	if (!shortCommit) {
		return prefix;
	}
	return `${prefix} (${shortCommit})`;
}
