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
	await expect(
		searchOrder(page).getByRole('button', { name: 'Move Lidarr up', exact: true })
	).toBeVisible();
}

function searchOrder(page: Page): Locator {
	return page.locator('[data-field="search_priority_preset"]');
}

async function keyboardMove(
	page: Page,
	order: 'search' | 'download',
	kind: string,
	steps: number
): Promise<void> {
	const button = page
		.locator(`[data-field="${order}_priority_preset"]`)
		.getByRole('button', { name: `Move ${kind} up`, exact: true });
	for (let step = 0; step < steps; step++) {
		await button.focus();
		await button.press('Enter');
	}
}

async function expectRank(page: Page, order: string, kind: string, rank: number): Promise<void> {
	await expect(page.locator(`#${order}-priority-${kind}`)).toHaveAttribute(
		'data-rank',
		String(rank)
	);
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
		await keyboardMove(page, 'search', 'Lidarr', 2);
		const save = page.getByRole('button', { name: 'Save', exact: true });
		await saveForm(page, SETTINGS, save);
		const edited = await readSettings(page);
		expect(edited.search_priority_preset).toEqual({
			lidarr: 10,
			sonarr: 20,
			radarr: 20,
			readarr: 40,
			whisparr_v2: 50,
			whisparr_v3: 50
		});
		expect(edited.search_priority_preset.lidarr).toBeLessThan(edited.search_priority_preset.sonarr);
		expect(edited.download_priority_preset).toEqual({ ...preset.download, lidarr: 37 });
		await page.reload();
		await expectRank(page, 'search', 'lidarr', 10);
		await expectRank(page, 'download', 'lidarr', 37);
		await page.getByRole('button', { name: 'Reset to shipped preset', exact: true }).click();
		await expectRank(page, 'search', 'lidarr', preset.search.lidarr);
		await expectRank(page, 'download', 'lidarr', preset.download.lidarr);
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
		// The list editor cannot express incomplete or out-of-range maps.
		await expect(searchOrder(page).getByRole('listitem')).toHaveCount(6);
		await expect(searchOrder(page).getByRole('spinbutton')).toHaveCount(0);
		const first = searchOrder(page).getByRole('listitem').first();
		const last = searchOrder(page).getByRole('listitem').last();
		await expect(first.getByRole('button', { name: / up$/ })).toBeDisabled();
		await expect(first.getByRole('checkbox')).toBeDisabled();
		await expect(last.getByRole('button', { name: / down$/ })).toBeDisabled();
		expect(await readSettings(page)).toEqual(before);
		await page.reload();
		await expectRank(page, 'search', 'lidarr', before.search_priority_preset.lidarr);
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

const NIGHT_SEARCH = {
	lidarr: 10,
	sonarr: 20,
	radarr: 20,
	readarr: 40,
	whisparr_v2: 50,
	whisparr_v3: 50
};
const NIGHT_DOWNLOAD = {
	readarr: 10,
	sonarr: 20,
	radarr: 20,
	lidarr: 40,
	whisparr_v2: 50,
	whisparr_v3: 50
};

async function createNightPreset(page: Page): Promise<PriorityPreset> {
	await keyboardMove(page, 'search', 'Lidarr', 2);
	await keyboardMove(page, 'download', 'Readarr (legacy)', 3);
	await page.getByLabel('Preset name', { exact: true }).fill('night');
	const [created] = await Promise.all([
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === '/api/v1/priority-presets' &&
				response.request().method() === 'POST'
		),
		page.getByRole('button', { name: 'Save current orders as preset', exact: true }).click()
	]);
	expect(created.status(), await created.text()).toBe(201);
	return (await created.json()) as PriorityPreset;
}

async function applyNightPreset(page: Page, saved: PriorityPreset): Promise<void> {
	const [applied] = await Promise.all([
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === `/api/v1/priority-presets/${saved.id}/apply`
		),
		page.getByRole('button', { name: 'Apply preset night', exact: true }).click()
	]);
	expect(applied.status(), await applied.text()).toBe(200);
	const active = await readSettings(page);
	expect(active.search_priority_preset).toEqual(saved.search);
	expect(active.download_priority_preset).toEqual(saved.download);
	await expectRank(page, 'search', 'lidarr', 10);
}

async function renameAndDeleteNight(page: Page, saved: PriorityPreset): Promise<void> {
	await page.getByRole('button', { name: 'Rename preset night', exact: true }).click();
	await page.locator(`#rename-${saved.id}`).fill('evening');
	const [renamed] = await Promise.all([
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === `/api/v1/priority-presets/${saved.id}` &&
				response.request().method() === 'PUT'
		),
		page.getByRole('button', { name: 'Save preset name', exact: true }).click()
	]);
	expect(renamed.status(), await renamed.text()).toBe(200);
	expect((await readSettings(page)).search_priority_preset).toEqual(saved.search);
	const shippedRow = page.locator('[data-preset="media_first"]');
	await expect(shippedRow.getByText('Shipped · read-only', { exact: true })).toBeVisible();
	await expect(shippedRow.getByRole('button', { name: /Rename|Delete/ })).toHaveCount(0);
	await settleToasts(page);
	const axe = await new AxeBuilder({ page }).include('#panel-caps').withTags(WCAG_22_AA).analyze();
	expect(axe.violations).toEqual([]);
	const [deleted] = await Promise.all([
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === `/api/v1/priority-presets/${saved.id}` &&
				response.request().method() === 'DELETE'
		),
		page.getByRole('button', { name: 'Delete preset evening', exact: true }).click()
	]);
	expect(deleted.status()).toBe(204);
	expect((await readSettings(page)).download_priority_preset).toEqual(saved.download);
}

async function savesAndAppliesNamedPreset({ page }: PageFixture): Promise<void> {
	const headers = await csrfHeaders(page);
	const original = await readSettings(page);
	const preset = await mediaFirst(page);
	let savedId: string | undefined;
	try {
		await putSettings(page, headers, {
			...original,
			search_priority_preset: preset.search,
			download_priority_preset: preset.download
		});
		await openPresets(page);
		const saved = await createNightPreset(page);
		savedId = saved.id;
		expect(savedId).toBeTruthy();
		expect(saved.search).toEqual(NIGHT_SEARCH);
		expect(saved.download).toEqual(NIGHT_DOWNLOAD);
		expect((await readSettings(page)).search_priority_preset).toEqual(preset.search);
		await applyNightPreset(page, saved);
		await renameAndDeleteNight(page, saved);
		savedId = undefined;
	} finally {
		try {
			if (savedId)
				expect(
					(await page.request.delete(`/api/v1/priority-presets/${savedId}`, { headers })).status()
				).toBe(204);
		} finally {
			await restorePresets(page, headers, original);
		}
	}
}

async function fillPresetSlots(
	page: Page,
	headers: Headers,
	preset: PriorityPreset,
	createdIds: string[]
): Promise<void> {
	const listed = await page.request.get('/api/v1/priority-presets');
	expect(listed.status()).toBe(200);
	const existing = ((await listed.json()) as PriorityPreset[]).filter(
		(entry) => !entry.builtin
	).length;
	for (let index = existing; index < 20; index++) {
		const created = await page.request.post('/api/v1/priority-presets', {
			headers,
			data: {
				name: `p-${crypto.randomUUID()}`,
				search: preset.search,
				download: preset.download
			}
		});
		expect(created.status(), await created.text()).toBe(201);
		const saved = (await created.json()) as PriorityPreset;
		if (!saved.id) throw new Error('created preset has no UUID');
		createdIds.push(saved.id);
	}
}

async function rejectsNamedPresetLimits({ page }: PageFixture): Promise<void> {
	const headers = await csrfHeaders(page);
	const preset = await mediaFirst(page);
	const createdIds: string[] = [];
	try {
		const longName = await page.request.post('/api/v1/priority-presets', {
			headers,
			data: {
				name: 'a'.repeat(41),
				search: preset.search,
				download: preset.download
			}
		});
		expect(longName.status()).toBe(422);
		await expectProblem(longName);
		await fillPresetSlots(page, headers, preset, createdIds);
		const overflow = await page.request.post('/api/v1/priority-presets', {
			headers,
			data: {
				name: 'twenty-first',
				search: preset.search,
				download: preset.download
			}
		});
		expect(overflow.status()).toBe(422);
		await expectProblem(overflow);
		await openPresets(page);
		await expect(
			page.getByRole('button', { name: 'Save current orders as preset', exact: true })
		).toBeDisabled();
	} finally {
		for (const id of createdIds) {
			const removed = await page.request.delete(`/api/v1/priority-presets/${id}`, { headers });
			expect(removed.status()).toBe(204);
		}
	}
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
		'rejects an incomplete preset and keeps list edits bounded without persisting',
		rejectsIncompletePreset
	);
	test(
		'accepts ranks 1 and 100 and keeps an equal rank as an instance override',
		acceptsBoundaryAndEqualRanks
	);
	test(
		'saves night, applies both maps and preserves active settings after rename and deletion',
		savesAndAppliesNamedPreset
	);
	test('rejects a 41-character name and the 21st user preset', rejectsNamedPresetLimits);
	test('has no axe violations in the preset settings section', accessiblePresets);
	test('has no axe violations on the instance page with its priority card', accessibleInstance);
});
