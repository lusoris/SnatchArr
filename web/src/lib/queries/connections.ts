// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createSentioQuery } from '@sveltesentio/query';
import { api, problemStatus } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { keys } from '#lib/query.js';

/**
 * Shared query pair for Prowlarr links and live indexer statuses.
 */
export function prowlarrQueries() {
	const links = createSentioQuery({
		queryKey: keys.prowlarrLinks,
		queryFn: async () => (await api.GET('/prowlarr')).data ?? []
	});

	const status = createSentioQuery({
		queryKey: keys.prowlarr,
		queryFn: async () => (await api.GET('/prowlarr/status')).data ?? [],
		refetchInterval: 30_000
	});

	return { links, status };
}

/**
 * Shared query pair for download clients and client statuses.
 */
export function downloadClientQueries() {
	const clients = createSentioQuery({
		queryKey: keys.dlclients,
		queryFn: async () => (await api.GET('/download-clients')).data ?? []
	});

	const status = createSentioQuery({
		queryKey: keys.dlstatus,
		queryFn: async () => (await api.GET('/download-clients/status')).data ?? [],
		refetchInterval: 30_000
	});

	return { clients, status };
}

/** Loads the latest observation while its client plan is open; 404 means not observed yet. */
export function downloadClientPriorityPlanQuery(clientId: () => string, enabled: () => boolean) {
	return createSentioQuery<components['schemas']['DownloadClientPriorityPlan'] | null>(() => ({
		queryKey: keys.dlpriorityPlan(clientId()),
		enabled: enabled(),
		refetchInterval: 30_000,
		queryFn: async () => {
			try {
				const result = await api.GET('/download-clients/{clientId}/priority-plan', {
					params: { path: { clientId: clientId() } }
				});
				return result.data ?? null;
			} catch (error) {
				if (problemStatus(error) === 404) return null;
				throw error;
			}
		}
	}));
}
