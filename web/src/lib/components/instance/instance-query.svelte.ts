// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createSentioQuery } from '@sveltesentio/query';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { keys } from '#lib/query.js';

export type Instance = components['schemas']['Instance'];

/**
 * Shared query for loading an instance by ID across detail layout and tab views.
 * Accepts a static ID string or a reactive getter.
 */
export function instanceQuery(id: string | (() => string)) {
	const resolveId = typeof id === 'function' ? id : () => id;
	return createSentioQuery<Instance | undefined>(() => {
		const instanceId = resolveId();
		return {
			queryKey: keys.instance(instanceId),
			queryFn: async () =>
				(await api.GET('/instances/{instanceId}', { params: { path: { instanceId } } })).data
		};
	});
}
