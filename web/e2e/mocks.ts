// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Signed-in admin over an empty fleet: the API state the mocked e2e suites share.
import type { Page } from '@playwright/test';

export function json(
	body: unknown,
	status = 200
): { status: number; contentType: string; body: string } {
	return { status, contentType: 'application/json', body: JSON.stringify(body) };
}

export function problemJson(
	status: number,
	detail: string,
	title = 'Error'
): {
	status: number;
	contentType: string;
	body: string;
} {
	return {
		status,
		contentType: 'application/problem+json',
		body: JSON.stringify({
			type: 'https://tools.ietf.org/html/rfc9457',
			title,
			status,
			detail
		})
	};
}

export const samplePolicyData = {
	missing_per_cycle: 10,
	upgrade_per_cycle: 5,
	cycle_interval_s: 300,
	hourly_cap: 50,
	selection: 'recent',
	monitored_only: true,
	skip_future_releases: true,
	radarr_release_type: 'physical',
	sonarr_missing_mode: 'episodes',
	sonarr_upgrade_mode: 'episodes',
	lidarr_missing_mode: 'album',
	processed_ttl_h: 24,
	max_queue_size: 100,
	await_command: false,
	page_size: 50,
	recent_grab_window_h: 48,
	afterglow_max_h: 168,
	release_delay_h: 0,
	skip_unavailable_movies: true,
	dry_run: false,
	include_tags: [],
	exclude_tags: [],
	include_quality_profiles: [],
	exclude_quality_profiles: []
};

export const sampleSettingsData = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	// The seeded default of migration 000025, so the preset fields render as on a real install.
	search_priority_preset: {
		sonarr: 10,
		radarr: 10,
		lidarr: 20,
		readarr: 30,
		whisparr_v2: 40,
		whisparr_v3: 40
	},
	download_priority_preset: {
		sonarr: 10,
		radarr: 10,
		lidarr: 20,
		readarr: 30,
		whisparr_v2: 40,
		whisparr_v3: 40
	}
};

export const sampleRuntimeSettingsData = [
	{
		key: 'port',
		env: 'APP_PORT',
		class: 'bootstrap',
		type: 'int',
		default: '8080',
		value: '8080',
		source: 'default',
		locked: true,
		apply: 'restart'
	},
	{
		key: 'log_level',
		env: 'APP_LOG_LEVEL',
		class: 'runtime',
		type: 'string',
		default: 'info',
		value: 'info',
		source: 'db',
		locked: false,
		apply: 'live'
	},
	{
		key: 'api_secret',
		env: 'APP_API_SECRET',
		class: 'secret',
		type: 'string',
		default: '',
		is_set: true,
		source: 'env',
		locked: true,
		apply: 'restart'
	}
];

/** Signed-in endpoints whose mock body is a fixed object. */
const FIXED_BODIES: Record<string, unknown> = {
	'adult/image-sets': { items: [], total: 0 },
	'adult/sync/status': {
		total_sources: 0,
		enabled_sources: 0,
		performers_synced: 0,
		studios_synced: 0
	},
	'configarr/status': { linked: false, watching: false },
	'requests/settings': {
		voting_mode: 'upvotes_only',
		allow_viewer_votes: true,
		visibility: 'public_attributed',
		voting_ttl_days: 0
	}
};

export async function mockSignedInApi(
	page: Page,
	role: 'admin' | 'requester' | 'viewer' = 'admin'
): Promise<void> {
	await page.route('**/api/v1/auth/setup/status', (route) =>
		route.fulfill(json({ setup_complete: true }))
	);
	await page.route('**/api/v1/auth/session', (route) =>
		route.fulfill(
			json({
				user: { id: '00000000-0000-0000-0000-000000000099', username: role, role },
				csrf_token: 'test-token',
				expires_at: '2099-01-01T00:00:00Z'
			})
		)
	);
	await page.route('**/api/v1/auth/sso', (route) =>
		route.fulfill(json({ oidc: false, proxy: false, jellyfin: true }))
	);
	await page.route('**/api/v1/auth/identities', (route) => route.fulfill(json({ items: [] })));
	for (const path of [
		'instances',
		'hourly-caps',
		'prowlarr/status',
		'prowlarr',
		'download-clients/status',
		'download-clients',
		'seerr',
		'runs',
		'events',
		'schedules',
		'system/modules',
		'adult/sources'
	]) {
		await page.route(`**/api/v1/${path}*`, (route) => route.fulfill(json([])));
	}
	for (const path of ['adult/performers', 'adult/studios']) {
		await page.route(`**/api/v1/${path}*`, (route) => route.fulfill(json({ items: [] })));
	}
	for (const [path, body] of Object.entries(FIXED_BODIES)) {
		await page.route(`**/api/v1/${path}*`, (route) => route.fulfill(json(body)));
	}
}

export async function mockModulesApi(
	page: Page,
	modules: Array<{ id: string; enabled: boolean }>
): Promise<void> {
	await page.route('**/api/v1/system/modules', (route) => route.fulfill(json(modules)));
}

// A few rows, the scale of the seeded stack: the budget measures page density, not list length.
const ADULT_ROWS = 3;

export const sampleAdultSources = Array.from({ length: ADULT_ROWS }, (_, i) => ({
	id: `00000000-0000-0000-0004-${String(i + 1).padStart(12, '0')}`,
	name: `Adult Source ${i + 1}`,
	endpoint: `https://source-${i + 1}.example.com/graphql`,
	has_api_key: true,
	enabled: true,
	sync_favorites: true,
	priority: i + 1,
	last_sync_at: '2026-10-21T06:19:00Z',
	created_at: '2026-10-21T06:19:00Z',
	updated_at: '2026-10-21T06:19:00Z'
}));

export const sampleAdultPerformers = Array.from({ length: ADULT_ROWS }, (_, i) => ({
	id: `00000000-0000-0000-0005-${String(i + 1).padStart(12, '0')}`,
	name: `Performer ${i + 1}`,
	canonical_name: `Performer ${i + 1}`,
	disambiguation: `Stage ${i + 1}`,
	urls: [`https://example.com/performer/${i + 1}`],
	is_favorite: i % 2 === 0,
	ref_count: (i + 1) * 3,
	created_at: '2026-10-21T06:19:00Z',
	updated_at: '2026-10-21T06:19:00Z'
}));

export const sampleAdultStudios = Array.from({ length: ADULT_ROWS }, (_, i) => ({
	id: `00000000-0000-0000-0006-${String(i + 1).padStart(12, '0')}`,
	name: `Studio ${i + 1}`,
	canonical_name: `Studio ${i + 1}`,
	urls: [`https://example.com/studio/${i + 1}`],
	is_favorite: i % 2 === 0,
	ref_count: (i + 1) * 2,
	created_at: '2026-10-21T06:19:00Z',
	updated_at: '2026-10-21T06:19:00Z'
}));

export const sampleAdultImageSets = Array.from({ length: ADULT_ROWS }, (_, i) => ({
	id: `00000000-0000-0000-0007-${String(i + 1).padStart(12, '0')}`,
	title: `Image Set ${i + 1}`,
	directory_path: `/data/adult/images/set-${i + 1}`,
	item_count: 10 + i,
	total_size_bytes: (i + 1) * 1024 * 1024 * 5,
	created_at: '2026-10-21T06:19:00Z',
	updated_at: '2026-10-21T06:19:00Z'
}));

export const sampleAdultImageItems = Array.from({ length: 12 }, (_, i) => ({
	id: `00000000-0000-0000-0008-${String(i + 1).padStart(12, '0')}`,
	image_set_id: sampleAdultImageSets[0].id,
	file_path: `/data/adult/images/set-1/${String(i + 1).padStart(3, '0')}.png`,
	file_name: `${String(i + 1).padStart(3, '0')}.png`,
	file_size_bytes: 1024 * 500,
	mime_type: 'image/png',
	sha256_checksum: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
	sort_order: i,
	created_at: '2026-10-21T06:19:00Z'
}));

export const sampleAdultImageSetDetail = {
	...sampleAdultImageSets[0],
	items: sampleAdultImageItems,
	ignore_file_present: true
};

export const adultRowExpectations: Record<string, string> = {
	'/adult/image-sets': 'Image Set 1',
	'/adult/performers': 'Performer 1',
	'/adult/sources': 'Adult Source 1',
	'/adult/studios': 'Studio 1'
};

export async function mockAdultListApi(page: Page): Promise<void> {
	await page.route('**/api/v1/adult/sources*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(json(sampleAdultSources));
	});
	await page.route('**/api/v1/adult/performers*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(json({ items: sampleAdultPerformers }));
	});
	await page.route('**/api/v1/adult/studios*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(json({ items: sampleAdultStudios }));
	});
	await page.route('**/api/v1/adult/image-sets*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(json({ items: sampleAdultImageSets, total: sampleAdultImageSets.length }));
	});
	// A glob `*` stops at `/`, so the detail path needs its own route.
	await page.route('**/api/v1/adult/image-sets/*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(json(sampleAdultImageSetDetail));
	});
	await page.route('**/api/v1/adult/sync/status*', (route) => {
		if (route.request().method() !== 'GET') return route.fallback();
		return route.fulfill(
			json({
				total_sources: sampleAdultSources.length,
				enabled_sources: sampleAdultSources.length,
				performers_synced: sampleAdultPerformers.length,
				studios_synced: sampleAdultStudios.length,
				last_sync_at: '2026-10-21T06:19:00Z'
			})
		);
	});
	await page.route('**/api/v1/adult/image-items/*/file', (route) =>
		route.fulfill({ status: 200, contentType: 'image/png', body: '' })
	);
}

export async function mockInstancePolicyApi(
	page: Page,
	instanceId: string,
	options?: {
		instanceKind?: 'sonarr' | 'radarr' | 'lidarr';
		policy?: typeof samplePolicyData;
		delayMs?: number;
		status?: number;
		saveDelayMs?: number;
		saveStatus?: number;
	}
): Promise<void> {
	await mockSignedInApi(page);
	const kind = options?.instanceKind ?? 'sonarr';
	await page.route(`**/api/v1/instances/${instanceId}`, (route) =>
		route.fulfill(
			json({
				id: instanceId,
				name: `${kind} Instance`,
				kind,
				base_url: `http://${kind}.local:8989`,
				source: 'manual',
				enabled: true,
				created_at: '2026-01-01T00:00:00Z',
				updated_at: '2026-01-01T00:00:00Z'
			})
		)
	);
	await page.route(`**/api/v1/instances/${instanceId}/policy`, async (route) => {
		if (route.request().method() === 'PUT') {
			if (options?.saveDelayMs) {
				await new Promise((r) => setTimeout(r, options.saveDelayMs));
			}
			if (options?.saveStatus && options.saveStatus >= 400) {
				await route.fulfill(problemJson(options.saveStatus, 'Policy save failure'));
				return;
			}
			const body = route.request().postDataJSON();
			await route.fulfill(json(body ?? options?.policy ?? samplePolicyData));
			return;
		}
		if (options?.delayMs) {
			await new Promise((r) => setTimeout(r, options.delayMs));
		}
		if (options?.status && options.status >= 400) {
			await route.fulfill(problemJson(options.status, 'Policy service failure'));
			return;
		}
		await route.fulfill(json(options?.policy ?? samplePolicyData));
	});
	await page.route(`**/api/v1/instances/${instanceId}/policy/filter-options`, (route) =>
		route.fulfill(json({ tags: [], quality_profiles: [] }))
	);
}

export async function mockSettingsApi(
	page: Page,
	options?: {
		settings?: typeof sampleSettingsData;
		delayMs?: number;
		status?: number;
		saveDelayMs?: number;
		saveStatus?: number;
		role?: 'admin' | 'requester' | 'viewer';
	}
): Promise<void> {
	await mockSignedInApi(page, options?.role);
	await page.route('**/api/v1/settings', async (route) => {
		if (route.request().method() === 'PUT') {
			if (options?.saveDelayMs) {
				await new Promise((r) => setTimeout(r, options.saveDelayMs));
			}
			if (options?.saveStatus && options.saveStatus >= 400) {
				await route.fulfill(problemJson(options.saveStatus, 'Settings save failure'));
				return;
			}
			const body = route.request().postDataJSON();
			await route.fulfill(json(body ?? options?.settings ?? sampleSettingsData));
			return;
		}
		if (options?.delayMs) {
			await new Promise((r) => setTimeout(r, options.delayMs));
		}
		if (options?.status && options.status >= 400) {
			await route.fulfill(problemJson(options.status, 'Settings service failure'));
			return;
		}
		await route.fulfill(json(options?.settings ?? sampleSettingsData));
	});
	await page.route('**/api/v1/notifications/channels', (route) => route.fulfill(json([])));
	await page.route('**/api/v1/notifications/rules', (route) => route.fulfill(json([])));
	await page.route('**/api/v1/priority-presets', (route) => route.fulfill(json([])));
	await mockRuntimeSettingsApi(page);
}

export async function mockRuntimeSettingsApi(
	page: Page,
	settings = sampleRuntimeSettingsData
): Promise<void> {
	await page.route('**/api/v1/settings/runtime', (route) => route.fulfill(json(settings)));
}

const sampleLicences = {
	generatedBy: { 'cargo-about': '0.9.2', 'go-licenses': '2.0.1', pnpm: '12.11.2' },
	packages: [
		{
			ecosystem: 'go',
			name: 'github.com/gin-gonic/gin',
			version: 'v1.10.0',
			licence: 'MIT',
			url: 'https://github.com/gin-gonic/gin',
			text: 'MIT License'
		}
	]
};

/**
 * Mocks the signed-in API, the system status and the static licences.json the About tab reads.
 * `delayMs` holds the licences response back, `status` >= 400 answers it with a problem document.
 */
export async function mockAboutApi(
	page: Page,
	options?: { delayMs?: number; status?: number }
): Promise<void> {
	await mockSignedInApi(page);
	await page.route('**/api/v1/system/status', (route) =>
		route.fulfill(
			json({
				name: 'SnatchArr',
				version: '0.1.0',
				commit: 'abc1234',
				build_date: '2026-10-20',
				profile: 'dev',
				setup_complete: true
			})
		)
	);
	await page.route('**/licences.json', async (route) => {
		if (options?.delayMs) {
			await new Promise((r) => setTimeout(r, options.delayMs));
		}
		if (options?.status && options.status >= 400) {
			await route.fulfill(problemJson(options.status, 'Licences unavailable'));
			return;
		}
		await route.fulfill(json(sampleLicences));
	});
}
