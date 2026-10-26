// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createSentioQuery } from '@sveltesentio/query';
import { api } from '#lib/api/client.js';
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
