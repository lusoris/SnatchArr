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
	policyFilterOptions: (id: string) => ['instances', id, 'policy', 'filter-options'] as const,
	queuePolicy: (id: string) => ['instances', id, 'queue-policy'] as const,
	queueStrikes: (id: string) => ['instances', id, 'queue-strikes'] as const,
	instancePriority: (id: string) => ['instances', id, 'priority'] as const,
	priorityPresets: ['priority-presets'] as const,
	caps: ['hourly-caps'] as const,
	runs: ['runs'] as const,
	instanceRuns: (id: string) => ['runs', id] as const,
	run: (id: string) => ['runs', 'single', id] as const,
	events: ['events'] as const,
	logs: (level: string, q: string, source: string) => ['logs', level, q, source] as const,
	schedules: ['schedules'] as const,
	dlclients: ['download-clients'] as const,
	dlstatus: ['download-clients', 'status'] as const,
	seerrLinks: ['seerr', 'links'] as const,
	seerrRequests: ['seerr', 'requests'] as const,
	prowlarrLinks: ['prowlarr', 'links'] as const,
	configarr: ['configarr', 'status'] as const,
	prowlarr: ['prowlarr', 'status'] as const,
	settings: ['settings'] as const,
	channels: ['notification-channels'] as const,
	rules: ['notification-rules'] as const,
	apiKeys: ['api-keys'] as const,
	identities: ['identities'] as const,
	ssoStatus: ['sso-status'] as const,
	systemStatus: ['system', 'status'] as const,
	licences: ['licences'] as const,
	modules: ['system', 'modules'] as const,
	module: (id: string) => ['system', 'modules', id] as const,
	watchawareGateSeries: (id: string) => ['watchaware', 'gate', 'series', id] as const,
	watchawareGateDecisions: (id: string) => ['watchaware', 'gate', 'decisions', id] as const
};
