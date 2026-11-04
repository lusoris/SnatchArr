// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #989

import { expect, test, type Locator, type Page } from '@playwright/test';
import { createInstance, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

const REQUEST_TIMEOUT_MS = 10_000;
const MAX_INSTANCES = 500;
const fixableAgedUrl = process.env.SNATCHARR_E2E_FAKEARR_FIXABLE ?? 'http://127.0.0.1:18992';

interface QueueRuleSetting {
	mode: 'observe' | 'act';
	threshold: number;
	blocklist?: boolean;
}

interface QueuePolicyPayload {
	enabled: boolean;
	grace_seconds: number;
	rules: {
		stalled: QueueRuleSetting;
		slow: QueueRuleSetting;
		failed: QueueRuleSetting;
		import_blocked: QueueRuleSetting;
		unwanted_score: QueueRuleSetting;
		unwanted_unmonitored: QueueRuleSetting;
		import_fixable?: QueueRuleSetting;
		id_matched?: QueueRuleSetting;
		metadata_stuck?: QueueRuleSetting;
		import_broken?: QueueRuleSetting;
		import_not_wanted?: QueueRuleSetting;
		not_an_upgrade?: QueueRuleSetting;
		duplicate?: QueueRuleSetting;
	};
}

interface FakeDeleteRecord {
	id: number;
	blocklist: boolean;
	remove_from_client: boolean;
	skip_redownload: boolean;
}

async function saveQueuePolicy(page: Page, instanceId: string): Promise<void> {
	const saveBtn = page.getByRole('button', {
		name: /save queue policy|warteschlangen-regeln speichern/i
	});
	const [putRes] = await Promise.all([
		page.waitForResponse(
			(r) =>
				r.url().includes(`/api/v1/instances/${instanceId}/queue-policy`) &&
				r.request().method() === 'PUT'
		),
		saveBtn.click()
	]);
	expect(putRes.ok()).toBeTruthy();
}

async function assertQueuePolicyUIInitial(page: Page, instanceId: string): Promise<Locator> {
	await page.goto(`/instances/${instanceId}/queue`);
	await expect(
		page.getByRole('heading', { level: 2, name: /queue management|warteschlangen-verwaltung/i })
	).toBeVisible();

	const fixableRow = page
		.locator('tbody tr')
		.filter({ hasText: /Fixable import issues|Behebbare Importprobleme/i });
	await expect(fixableRow).toBeVisible();

	const blocklistCheckbox = fixableRow.getByRole('checkbox', {
		name: /blocklist|sperrliste/i
	});
	await expect(blocklistCheckbox).toBeVisible();
	await expect(blocklistCheckbox).toBeChecked();

	const idMatchedRow = page
		.locator('tbody tr')
		.filter({ hasText: /Force-import releases matched by ID|Per ID zugeordnete Releases/i });
	await expect(idMatchedRow).toBeVisible();
	await expect(idMatchedRow.getByRole('checkbox', { name: /blocklist|sperrliste/i })).toHaveCount(
		0
	);

	return blocklistCheckbox;
}

async function fetchFakeDeletes(page: Page, baseUrl: string): Promise<FakeDeleteRecord[]> {
	const res = await page.request.get(`${baseUrl}/_fake/deletes`, {
		timeout: REQUEST_TIMEOUT_MS
	});
	expect(res.status()).toBe(200);
	return (await res.json()) as FakeDeleteRecord[];
}

// createFixableInstance removes instances a previous aborted run left on the aged fakearr,
// then registers a fresh sonarr instance pointing at it and returns its id.
async function createFixableInstance(page: Page, csrf: string): Promise<string> {
	const listRes = await page.request.get('/api/v1/instances', { timeout: REQUEST_TIMEOUT_MS });
	if (listRes.ok()) {
		const all = (await listRes.json()) as Array<{ id: string; base_url: string }>;
		for (const inst of all.slice(0, MAX_INSTANCES)) {
			if (inst.base_url === fixableAgedUrl) {
				await deleteRows(page, csrf, 'instances', [inst.id]);
			}
		}
	}
	const createRes = await page.request.post('/api/v1/instances', {
		headers: { 'X-CSRF-Token': csrf },
		data: {
			name: `e2e-989-giveup-${uniqueSuffix()}`,
			kind: 'sonarr',
			base_url: fixableAgedUrl,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		},
		timeout: REQUEST_TIMEOUT_MS
	});
	expect(createRes.status()).toBe(201);
	return ((await createRes.json()) as { id: string }).id;
}

// setFixableActPolicy enables the queue policy with no grace period and sets import_fixable
// to act with threshold 1 and blocklist on.
async function setFixableActPolicy(page: Page, csrf: string, instanceId: string): Promise<void> {
	const url = `/api/v1/instances/${instanceId}/queue-policy`;
	const getRes = await page.request.get(url, { timeout: REQUEST_TIMEOUT_MS });
	expect(getRes.status()).toBe(200);
	const policy = (await getRes.json()) as QueuePolicyPayload;
	policy.enabled = true;
	policy.grace_seconds = 0;
	policy.rules.import_fixable = { mode: 'act', threshold: 1, blocklist: true };
	const putRes = await page.request.put(url, {
		headers: { 'X-CSRF-Token': csrf },
		data: policy,
		timeout: REQUEST_TIMEOUT_MS
	});
	expect(putRes.status()).toBe(200);
}

test('989: queue policy screen shows blocklist control for import_fixable, fresh policy reads blocklist=true', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `e2e-989-${uniqueSuffix()}`;
	const instanceId = await createInstance(page, csrf, name);

	try {
		// Fresh policy reads blocklist=true for import_fixable via the queue-policy API.
		const freshRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(freshRes.status()).toBe(200);
		const freshPolicy = (await freshRes.json()) as QueuePolicyPayload;
		expect(freshPolicy.rules.import_fixable?.blocklist).toBe(true);

		// Queue policy screen shows the blocklist control for the import_fixable rule.
		const blocklistCheckbox = await assertQueuePolicyUIInitial(page, instanceId);

		// Toggle blocklist off in the UI, save, and verify via API.
		await blocklistCheckbox.click();
		await expect(blocklistCheckbox).not.toBeChecked();
		await saveQueuePolicy(page, instanceId);

		const readOffRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(readOffRes.status()).toBe(200);
		const readOffPolicy = (await readOffRes.json()) as QueuePolicyPayload;
		expect(readOffPolicy.rules.import_fixable?.blocklist).toBe(false);

		// Toggle blocklist back on, save, and verify restored via API.
		await blocklistCheckbox.click();
		await expect(blocklistCheckbox).toBeChecked();
		await saveQueuePolicy(page, instanceId);

		const restoredRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(restoredRes.status()).toBe(200);
		const restoredPolicy = (await restoredRes.json()) as QueuePolicyPayload;
		expect(restoredPolicy.rules.import_fixable?.blocklist).toBe(true);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});

test('989: negative - queue policy rejects unknown instance or invalid threshold with problem+json', async ({
	page
}) => {
	const csrf = await signIn(page);
	const unknownId = '00000000-0000-0000-0000-000000000000';

	// Unknown instance GET returns 404 problem+json.
	const getRes = await page.request.get(`/api/v1/instances/${unknownId}/queue-policy`, {
		timeout: REQUEST_TIMEOUT_MS
	});
	expect(getRes.status()).toBe(404);
	expect(getRes.headers()['content-type']).toContain('application/problem+json');
	const getProblem = (await getRes.json()) as { status: number };
	expect(getProblem.status).toBe(404);

	// Invalid threshold 0 on a valid instance returns 400 or 422 problem+json.
	const name = `e2e-989-neg-${uniqueSuffix()}`;
	const instanceId = await createInstance(page, csrf, name);

	try {
		const validRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(validRes.status()).toBe(200);
		const policy = (await validRes.json()) as QueuePolicyPayload;

		const invalidPolicy = {
			...policy,
			rules: {
				...policy.rules,
				import_fixable: { mode: 'observe' as const, threshold: 0, blocklist: true }
			}
		};
		const putRes = await page.request.put(`/api/v1/instances/${instanceId}/queue-policy`, {
			headers: { 'X-CSRF-Token': csrf },
			data: invalidPolicy,
			timeout: REQUEST_TIMEOUT_MS
		});
		expect([400, 422]).toContain(putRes.status());
		expect(putRes.headers()['content-type']).toContain('application/problem+json');
		const putProblem = (await putRes.json()) as { status: number };
		expect([400, 422]).toContain(putProblem.status);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});

test('989: boundary - import_fixable accepts minimum threshold 1 and maximum threshold 100', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `e2e-989-bnd-${uniqueSuffix()}`;
	const instanceId = await createInstance(page, csrf, name);

	try {
		const getRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(getRes.status()).toBe(200);
		const basePolicy = (await getRes.json()) as QueuePolicyPayload;

		// Boundary min: threshold 1
		const minPolicy = {
			...basePolicy,
			rules: {
				...basePolicy.rules,
				import_fixable: { mode: 'observe' as const, threshold: 1, blocklist: true }
			}
		};
		const minRes = await page.request.put(`/api/v1/instances/${instanceId}/queue-policy`, {
			headers: { 'X-CSRF-Token': csrf },
			data: minPolicy,
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(minRes.status()).toBe(200);
		const minRead = (await minRes.json()) as QueuePolicyPayload;
		expect(minRead.rules.import_fixable?.threshold).toBe(1);
		expect(minRead.rules.import_fixable?.blocklist).toBe(true);

		// Boundary max: threshold 1440
		const maxPolicy = {
			...basePolicy,
			rules: {
				...basePolicy.rules,
				import_fixable: { mode: 'act' as const, threshold: 1440, blocklist: true }
			}
		};
		const maxRes = await page.request.put(`/api/v1/instances/${instanceId}/queue-policy`, {
			headers: { 'X-CSRF-Token': csrf },
			data: maxPolicy,
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(maxRes.status()).toBe(200);
		const maxRead = (await maxRes.json()) as QueuePolicyPayload;
		expect(maxRead.rules.import_fixable?.threshold).toBe(1440);
		expect(maxRead.rules.import_fixable?.blocklist).toBe(true);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});

test('989: give-up - aged import_fixable item (>24h) is removed with blocklist=true and re-search requested via skipRedownload=false', async ({
	page
}) => {
	const csrf = await signIn(page);
	const instanceId = await createFixableInstance(page, csrf);

	try {
		// Reset the fake so retries see the record again, and take the baseline before the
		// act policy exists, so no sweep can delete between reset and baseline.
		const resetRes = await page.request.post(`${fixableAgedUrl}/_fake/reset`, {
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(resetRes.status()).toBe(204);

		// Baseline: the fake keeps deletes from earlier tests, so only a new record counts.
		const before = (await fetchFakeDeletes(page, fixableAgedUrl)).length;

		await setFixableActPolicy(page, csrf, instanceId);

		// Trigger queue sweep through served API.
		const sweepRes = await page.request.post(`/api/v1/instances/${instanceId}/queue/sweep`, {
			headers: { 'X-CSRF-Token': csrf },
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(sweepRes.status()).toBe(204);

		// Assert item was removed via DELETE with blocklist=true and skip_redownload=false:
		// skipRedownload=false is the re-search request the *arr runs itself (arrclient/queue.go).
		const deletes = await fetchFakeDeletes(page, fixableAgedUrl);
		expect(deletes.length).toBeGreaterThan(before);
		const del = deletes.at(-1)!;
		expect(del.id).toBe(1);
		expect(del.blocklist).toBe(true);
		expect(del.remove_from_client).toBe(true);
		expect(del.skip_redownload).toBe(false);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});
