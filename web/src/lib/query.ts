// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createQueryClient } from '@sveltesentio/query';

/** One QueryClient for the SPA: RFC 9457-aware retries, 15 s stale time. */
export const queryClient = createQueryClient({ staleTime: 15_000 });

/** Query keys, kept in one place so invalidation never guesses. */
export const keys = {
	instances: ['instances'] as const,
	instance: (id: string) => ['instances', id] as const,
	policy: (id: string) => ['instances', id, 'policy'] as const,
	caps: ['hourly-caps'] as const,
	runs: ['runs'] as const,
	events: ['events'] as const,
	schedules: ['schedules'] as const,
	dlclients: ['download-clients'] as const,
	dlstatus: ['download-clients', 'status'] as const,
	seerrRequests: ['seerr', 'requests'] as const,
	cleanuparr: ['cleanuparr', 'status'] as const,
	settings: ['settings'] as const
};
