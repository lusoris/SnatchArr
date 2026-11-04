// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #989

import { expect, test } from '@playwright/test';
import { createInstance, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

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

test('989: queue policy screen shows blocklist control for import_fixable, fresh policy reads blocklist=true', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `e2e-989-${uniqueSuffix()}`;
	const instanceId = await createInstance(page, csrf, name);

	try {
		// Fresh policy reads blocklist=true for import_fixable via the queue-policy API.
		const freshRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
		expect(freshRes.status()).toBe(200);
		const freshPolicy = (await freshRes.json()) as QueuePolicyPayload;
		expect(freshPolicy.rules.import_fixable?.blocklist).toBe(true);

		// Queue policy screen shows the blocklist control for the import_fixable rule.
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

		// id_matched rule imports and never removes, so it has no blocklist control.
		const idMatchedRow = page
			.locator('tbody tr')
			.filter({ hasText: /Force-import releases matched by ID|Per ID zugeordnete Releases/i });
		await expect(idMatchedRow).toBeVisible();
		await expect(idMatchedRow.getByRole('checkbox', { name: /blocklist|sperrliste/i })).toHaveCount(
			0
		);

		// Toggle blocklist off in the UI, save, and verify via API.
		await blocklistCheckbox.click();
		await expect(blocklistCheckbox).not.toBeChecked();

		const saveBtn = page.getByRole('button', {
			name: /save queue policy|warteschlangen-regeln speichern/i
		});
		const [putOffRes] = await Promise.all([
			page.waitForResponse(
				(r) =>
					r.url().includes(`/api/v1/instances/${instanceId}/queue-policy`) &&
					r.request().method() === 'PUT'
			),
			saveBtn.click()
		]);
		expect(putOffRes.ok()).toBeTruthy();

		const readOffRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
		expect(readOffRes.status()).toBe(200);
		const readOffPolicy = (await readOffRes.json()) as QueuePolicyPayload;
		expect(readOffPolicy.rules.import_fixable?.blocklist).toBe(false);

		// Toggle blocklist back on, save, and verify restored via API.
		await blocklistCheckbox.click();
		await expect(blocklistCheckbox).toBeChecked();

		const [putOnRes] = await Promise.all([
			page.waitForResponse(
				(r) =>
					r.url().includes(`/api/v1/instances/${instanceId}/queue-policy`) &&
					r.request().method() === 'PUT'
			),
			saveBtn.click()
		]);
		expect(putOnRes.ok()).toBeTruthy();

		const restoredRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
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
	const getRes = await page.request.get(`/api/v1/instances/${unknownId}/queue-policy`);
	expect(getRes.status()).toBe(404);
	expect(getRes.headers()['content-type']).toContain('application/problem+json');
	const getProblem = (await getRes.json()) as { status: number };
	expect(getProblem.status).toBe(404);

	// Invalid threshold 0 on a valid instance returns 400 or 422 problem+json.
	const name = `e2e-989-neg-${uniqueSuffix()}`;
	const instanceId = await createInstance(page, csrf, name);

	try {
		const validRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
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
			data: invalidPolicy
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
		const getRes = await page.request.get(`/api/v1/instances/${instanceId}/queue-policy`);
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
			data: minPolicy
		});
		expect(minRes.status()).toBe(200);
		const minRead = (await minRes.json()) as QueuePolicyPayload;
		expect(minRead.rules.import_fixable?.threshold).toBe(1);
		expect(minRead.rules.import_fixable?.blocklist).toBe(true);

		// Boundary max: threshold 100
		const maxPolicy = {
			...basePolicy,
			rules: {
				...basePolicy.rules,
				import_fixable: { mode: 'act' as const, threshold: 100, blocklist: true }
			}
		};
		const maxRes = await page.request.put(`/api/v1/instances/${instanceId}/queue-policy`, {
			headers: { 'X-CSRF-Token': csrf },
			data: maxPolicy
		});
		expect(maxRes.status()).toBe(200);
		const maxRead = (await maxRes.json()) as QueuePolicyPayload;
		expect(maxRead.rules.import_fixable?.threshold).toBe(100);
		expect(maxRead.rules.import_fixable?.blocklist).toBe(true);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});
