// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #559 #504

import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIResponse, type Locator, type Page } from '@playwright/test';
import type { components } from '../../src/lib/api/schema.js';
import { settleToasts } from '../settle.js';

type Settings = components['schemas']['Settings'];
type PriorityPreset = components['schemas']['PriorityPreset'];
type InstancePriority = components['schemas']['InstancePriority'];
type Headers = Record<string, string>;
type PageFixture = { page: Page };

const SETTINGS = '/api/v1/settings';
const WCAG_22_AA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

async function csrfHeaders(page: Page): Promise<Headers> {
	const response = await page.request.get('/api/v1/auth/session');
	expect(response.status()).toBe(200);
	const session = (await response.json()) as { csrf_token: string };
	return { 'X-CSRF-Token': session.csrf_token };
}

async function readSettings(page: Page): Promise<Settings> {
	const response = await page.request.get(SETTINGS);
	expect(response.status()).toBe(200);
	return (await response.json()) as Settings;
}

async function mediaFirst(page: Page): Promise<PriorityPreset> {
	const response = await page.request.get('/api/v1/priority-presets');
	expect(response.status()).toBe(200);
	const presets = (await response.json()) as PriorityPreset[];
	const preset = presets.find((entry) => entry.name === 'media_first');
	if (!preset) throw new Error('GET /priority-presets is missing media_first');
	return preset;
}

async function putSettings(page: Page, headers: Headers, data: Settings): Promise<void> {
	const response = await page.request.put(SETTINGS, { headers, data });
	expect(response.status(), await response.text()).toBe(200);
}

async function restorePresets(page: Page, headers: Headers, original: Settings): Promise<void> {
	// Preserve unrelated settings another feature test may have changed.
	await putSettings(page, headers, {
		...(await readSettings(page)),
		search_priority_preset: original.search_priority_preset,
		download_priority_preset: original.download_priority_preset
	});
	const restored = await readSettings(page);
	expect(restored.search_priority_preset).toEqual(original.search_priority_preset);
	expect(restored.download_priority_preset).toEqual(original.download_priority_preset);
}

async function openPresets(page: Page): Promise<void> {
	// The shipped settings section moved to Automation (ADR-0029).
	await page.goto('/automation?tab=caps');
	await expect(
		page.getByRole('heading', { name: 'Instance priority presets', exact: true })
	).toBeVisible();
	await expect(page.locator('#search-priority-lidarr')).toBeEnabled();
}

function priorityCard(page: Page): Locator {
	return page.getByRole('form', { name: 'Instance priority', exact: true });
}

async function saveForm(page: Page, path: string, button: Locator): Promise<void> {
	const [response] = await Promise.all([
		page.waitForResponse(
			(res) => new URL(res.url()).pathname === path && res.request().method() === 'PUT'
		),
		button.click()
	]);
	expect(response.status(), await response.text()).toBe(200);
	await expect(button).toBeEnabled();
}

async function readPriority(page: Page, id: string): Promise<InstancePriority> {
	const response = await page.request.get(`/api/v1/instances/${id}/priority`);
	expect(response.status()).toBe(200);
	return (await response.json()) as InstancePriority;
}

async function expectPriority(
	page: Page,
	id: string,
	search: number | null,
	rank: number,
	source: InstancePriority['search_source']
): Promise<void> {
	expect(await readPriority(page, id)).toEqual({
		search_priority: search,
		download_priority: null,
		effective_search_priority: rank,
		effective_download_priority: 10,
		search_source: source,
		download_source: 'preset'
	});
	const card = priorityCard(page);
	await expect(
		card.getByText(`Effective search rank: ${rank} (${source})`, { exact: true })
	).toBeVisible();
	await expect(
		card.getByText('Effective download rank: 10 (preset)', { exact: true })
	).toBeVisible();
	await expect(
		card.getByRole('spinbutton', { name: 'Download priority override', exact: true })
	).toHaveValue('');
}

async function withInstance(
	page: Page,
	run: (id: string, headers: Headers) => Promise<void>
): Promise<void> {
	const headers = await csrfHeaders(page);
	const original = await readSettings(page);
	const preset = await mediaFirst(page);
	const fakearr = process.env.SNATCHARR_E2E_FAKEARR;
	if (!fakearr) throw new Error('missing environment variable SNATCHARR_E2E_FAKEARR');
	let id: string | undefined;
	try {
		await putSettings(page, headers, {
			...original,
			search_priority_preset: preset.search,
			download_priority_preset: preset.download
		});
		const name = `e2e-priority-${crypto.randomUUID()}`;
		const response = await page.request.post('/api/v1/instances', {
			headers,
			data: {
				kind: 'sonarr',
				name,
				base_url: `${fakearr}/${name}`,
				api_key: 'fake-api-key-0123456789',
				enabled: true
			}
		});
		expect(response.status(), await response.text()).toBe(201);
		id = ((await response.json()) as { id: string }).id;
		await page.goto(`/instances/${id}`);
		await expectPriority(page, id, null, 10, 'preset');
		await run(id, headers);
	} finally {
		try {
			if (id) {
				const removed = await page.request.delete(`/api/v1/instances/${id}`, { headers });
				expect(removed.ok()).toBeTruthy();
			}
		} finally {
			await restorePresets(page, headers, original);
		}
	}
}

async function expectProblem(response: APIResponse): Promise<void> {
	expect(response.status()).toBeGreaterThanOrEqual(400);
	expect(response.status()).toBeLessThan(500);
	expect(response.headers()['content-type']).toMatch(/^application\/problem\+json\b/);
	const problem = (await response.json()) as components['schemas']['Problem'];
	expect(problem.status).toBe(response.status());
}

async function expectInvalidInput(input: Locator, button: Locator): Promise<void> {
	await input.fill('101');
	await button.click();
	await expect(input).toBeFocused();
	const validation = await input.evaluate((element: HTMLInputElement) => ({
		overflow: element.validity.rangeOverflow,
		message: element.validationMessage
	}));
	expect(validation.overflow).toBe(true);
	expect(validation.message).not.toBe('');
}

async function editsAndResetsPresets({ page }: PageFixture): Promise<void> {
	const headers = await csrfHeaders(page);
	const original = await readSettings(page);
	const preset = await mediaFirst(page);
	try {
		// Seed different orders so a coupled search/download save cannot pass accidentally.
		await putSettings(page, headers, {
			...original,
			search_priority_preset: preset.search,
			download_priority_preset: { ...preset.download, lidarr: 37 }
		});
		await openPresets(page);
		await page.locator('#search-priority-lidarr').fill('5');
		const save = page.getByRole('button', { name: 'Save', exact: true });
		await saveForm(page, SETTINGS, save);
		const edited = await readSettings(page);
		expect(edited.search_priority_preset).toEqual({ ...preset.search, lidarr: 5 });
		expect(edited.download_priority_preset).toEqual({ ...preset.download, lidarr: 37 });
		await page.reload();
		await expect(page.locator('#search-priority-lidarr')).toHaveValue('5');
		await expect(page.locator('#download-priority-lidarr')).toHaveValue('37');
		await page.getByRole('button', { name: 'Reset to shipped preset', exact: true }).click();
		await expect(page.locator('#search-priority-lidarr')).toHaveValue(String(preset.search.lidarr));
		await expect(page.locator('#download-priority-lidarr')).toHaveValue(
			String(preset.download.lidarr)
		);
		await saveForm(page, SETTINGS, save);
		const reset = await readSettings(page);
		expect(reset.search_priority_preset).toEqual(preset.search);
		expect(reset.download_priority_preset).toEqual(preset.download);
	} finally {
		await restorePresets(page, headers, original);
	}
}

async function overridesAndClearsSearch({ page }: PageFixture): Promise<void> {
	await withInstance(page, async (id) => {
		const card = priorityCard(page);
		const search = card.getByRole('spinbutton', { name: 'Search priority override', exact: true });
		await expect(search).toHaveValue('');
		await search.fill('1');
		await saveForm(
			page,
			`/api/v1/instances/${id}/priority`,
			card.getByRole('button', { name: 'Save priority', exact: true })
		);
		await expectPriority(page, id, 1, 1, 'instance');
		await page.reload();
		await expect(search).toHaveValue('1');
		await expectPriority(page, id, 1, 1, 'instance');
		await saveForm(
			page,
			`/api/v1/instances/${id}/priority`,
			card.getByRole('button', { name: 'Clear overrides', exact: true })
		);
		await expectPriority(page, id, null, 10, 'preset');
		await expect(search).toHaveValue('');
	});
}

async function rejectsInvalidOverride({ page }: PageFixture): Promise<void> {
	await withInstance(page, async (id, headers) => {
		const before = await readPriority(page, id);
		const response = await page.request.put(`/api/v1/instances/${id}/priority`, {
			headers,
			data: { search_priority: 101, download_priority: null }
		});
		await expectProblem(response);
		expect(await readPriority(page, id)).toEqual(before);
		const card = priorityCard(page);
		await expectInvalidInput(
			card.getByRole('spinbutton', { name: 'Search priority override', exact: true }),
			card.getByRole('button', { name: 'Save priority', exact: true })
		);
		expect(await readPriority(page, id)).toEqual(before);
		await page.reload();
		await expectPriority(page, id, null, 10, 'preset');
		await expect(
			card.getByRole('spinbutton', { name: 'Search priority override', exact: true })
		).toHaveValue('');
	});
}

async function rejectsIncompletePreset({ page }: PageFixture): Promise<void> {
	const headers = await csrfHeaders(page);
	const before = await readSettings(page);
	try {
		const incomplete = { ...before.search_priority_preset };
		delete incomplete.lidarr;
		const response = await page.request.put(SETTINGS, {
			headers,
			data: { ...before, search_priority_preset: incomplete }
		});
		await expectProblem(response);
		expect(await readSettings(page)).toEqual(before);
		await openPresets(page);
		await expectInvalidInput(
			page.locator('#search-priority-lidarr'),
			page.getByRole('button', { name: 'Save', exact: true })
		);
		expect(await readSettings(page)).toEqual(before);
		await page.reload();
		await expect(page.locator('#search-priority-lidarr')).toHaveValue(
			String(before.search_priority_preset.lidarr)
		);
	} finally {
		await restorePresets(page, headers, before);
	}
}

async function acceptsBoundaryAndEqualRanks({ page }: PageFixture): Promise<void> {
	await withInstance(page, async (id) => {
		const card = priorityCard(page);
		for (const rank of [1, 100, 10]) {
			await card
				.getByRole('spinbutton', { name: 'Search priority override', exact: true })
				.fill(String(rank));
			await saveForm(
				page,
				`/api/v1/instances/${id}/priority`,
				card.getByRole('button', { name: 'Save priority', exact: true })
			);
			await expectPriority(page, id, rank, rank, 'instance');
		}
		await page.reload();
		await expectPriority(page, id, 10, 10, 'instance');
	});
}

async function accessiblePresets({ page }: PageFixture): Promise<void> {
	await openPresets(page);
	await settleToasts(page);
	const results = await new AxeBuilder({ page })
		.include('#panel-caps')
		.withTags(WCAG_22_AA)
		.analyze();
	expect(results.violations).toEqual([]);
}

async function accessibleInstance({ page }: PageFixture): Promise<void> {
	await withInstance(page, async () => {
		await settleToasts(page);
		const results = await new AxeBuilder({ page }).withTags(WCAG_22_AA).analyze();
		expect(results.violations).toEqual([]);
	});
}

test.describe('559: shipped instance priority UI', () => {
	test.describe.configure({ mode: 'serial' });
	test('edits search independently and resets both orders to media_first', editsAndResetsPresets);
	test(
		'saves and clears a search override while downloads follow the preset',
		overridesAndClearsSearch
	);
	test('rejects rank 101 through the API and UI without persisting it', rejectsInvalidOverride);
	test(
		'rejects an incomplete preset and an invalid UI rank without persisting',
		rejectsIncompletePreset
	);
	test(
		'accepts ranks 1 and 100 and keeps an equal rank as an instance override',
		acceptsBoundaryAndEqualRanks
	);
	test('has no axe violations in the preset settings section', accessiblePresets);
	test('has no axe violations on the instance page with its priority card', accessibleInstance);
});
