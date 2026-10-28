// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { m } from '#lib/paraglide/messages.js';

export interface HelpEntry {
	summaryKey: string;
	docsPath: string;
}

export interface DocsEntry {
	title: () => string;
	docsPath: string;
}

export const HELP_MAP: Record<string, HelpEntry> = {
	'/': {
		summaryKey: 'help_overview',
		docsPath: 'concepts.md'
	},
	'/activity': {
		summaryKey: 'help_activity',
		docsPath: 'concepts.md#history-and-live-events'
	},
	'/instances': {
		summaryKey: 'help_instances',
		docsPath: 'concepts.md#snatch'
	},
	'/instances/[id]': {
		summaryKey: 'help_instance_overview',
		docsPath: 'concepts.md#stamina'
	},
	'/instances/[id]/policy': {
		summaryKey: 'help_instance_policy',
		docsPath: 'configuration.md#snatch-policy'
	},
	'/instances/[id]/queue': {
		summaryKey: 'help_instance_queue',
		docsPath: 'queue-management.md'
	},
	'/instances/[id]/activity': {
		summaryKey: 'help_instance_activity',
		docsPath: 'concepts.md#history-and-live-events'
	},
	'/instances/[id]/memory': {
		summaryKey: 'help_instance_memory',
		docsPath: 'concepts.md#afterglow'
	},
	'/automation': {
		summaryKey: 'help_automation',
		docsPath: 'concepts.md#schedules'
	},
	'/connections': {
		summaryKey: 'help_connections',
		docsPath: 'connections.md'
	},
	'/connections/download-clients': {
		summaryKey: 'help_connections_download_clients',
		docsPath: 'download-clients.md'
	},
	'/connections/seerr': {
		summaryKey: 'help_connections_seerr',
		docsPath: 'seerr.md'
	},
	'/connections/prowlarr': {
		summaryKey: 'help_connections_prowlarr',
		docsPath: 'prowlarr.md'
	},
	'/connections/configarr': {
		summaryKey: 'help_connections_configarr',
		docsPath: 'configuration.md#configarr'
	},
	'/adult/sources': {
		summaryKey: 'help_adult_sources',
		docsPath: 'modules.md'
	},
	'/adult/performers': {
		summaryKey: 'help_adult_performers',
		docsPath: 'modules.md'
	},
	'/adult/studios': {
		summaryKey: 'help_adult_studios',
		docsPath: 'modules.md'
	},
	'/system': {
		summaryKey: 'help_system',
		docsPath: 'configuration.md#runtime-settings'
	},
	'/settings/requests': {
		summaryKey: 'help_requests_settings',
		docsPath: 'configuration.md#runtime-settings'
	},
	'/system?tab=modules': {
		summaryKey: 'help_system_modules',
		docsPath: 'modules.md'
	},
	'/system?tab=notifications': {
		summaryKey: 'help_system_notifications',
		docsPath: 'notifications.md'
	},
	'/system?tab=about': {
		summaryKey: 'help_system_about',
		docsPath: 'index.md'
	},
	'/account': {
		summaryKey: 'help_account',
		docsPath: 'api.md#authentication'
	},
	'/login': {
		summaryKey: 'help_login',
		docsPath: 'sso.md'
	},
	'/setup': {
		summaryKey: 'help_setup',
		docsPath: 'install-compose.md'
	}
};

/**
 * Distinct documentation pages indexed for the global command palette (Docs: <page title>).
 */
export const DOCS_ENTRIES: DocsEntry[] = [
	{ title: () => m.docs_entry_overview_concepts(), docsPath: 'concepts.md' },
	{
		title: () => m.docs_entry_history_live_events(),
		docsPath: 'concepts.md#history-and-live-events'
	},
	{ title: () => m.docs_entry_snatch_pacing(), docsPath: 'concepts.md#snatch' },
	{ title: () => m.docs_entry_stamina_caps(), docsPath: 'concepts.md#stamina' },
	{ title: () => m.docs_entry_afterglow_memory(), docsPath: 'concepts.md#afterglow' },
	{ title: () => m.docs_entry_schedules(), docsPath: 'concepts.md#schedules' },
	{ title: () => m.docs_entry_snatch_policy(), docsPath: 'configuration.md#snatch-policy' },
	{ title: () => m.docs_entry_runtime_settings(), docsPath: 'configuration.md#runtime-settings' },
	{ title: () => m.docs_entry_configarr(), docsPath: 'configuration.md#configarr' },
	{ title: () => m.docs_entry_queue_management(), docsPath: 'queue-management.md' },
	{ title: () => m.docs_entry_connections(), docsPath: 'connections.md' },
	{ title: () => m.docs_entry_download_clients(), docsPath: 'download-clients.md' },
	{ title: () => m.docs_entry_seerr_integration(), docsPath: 'seerr.md' },
	{ title: () => m.docs_entry_prowlarr_integration(), docsPath: 'prowlarr.md' },
	{ title: () => m.docs_entry_optional_modules(), docsPath: 'modules.md' },
	{ title: () => m.docs_entry_notifications(), docsPath: 'notifications.md' },
	{ title: () => m.docs_entry_about_snatcharr(), docsPath: 'index.md' },
	{ title: () => m.docs_entry_api_authentication(), docsPath: 'api.md#authentication' },
	{ title: () => m.docs_entry_single_sign_on(), docsPath: 'sso.md' },
	{ title: () => m.docs_entry_installation_setup(), docsPath: 'install-compose.md' }
];

export function getHelpSummary(key: string): string {
	const messages = m as unknown as Record<string, (() => string) | undefined>;
	const fn = messages[key];
	return typeof fn === 'function' ? fn() : '';
}
