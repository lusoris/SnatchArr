// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #884

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { VIEWPORTS } from '../layout.js';
import { assertNoHorizontalScroll } from '../screen-range.js';

const execFileAsync = promisify(execFile);

function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`${name} unset: run through tools/e2e/web-e2e-api.sh`);
	}
	return value;
}

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	const data = (await res.json()) as { csrf_token: string };
	return data.csrf_token;
}

// Dialog-level fit; the page-level overflow check is the shared screen-range helper.
async function expectDialogFitsViewport(page: Page, dialog: Locator): Promise<void> {
	await expect(dialog).toBeVisible();
	await expect(dialog).toBeInViewport({ ratio: 1 });
	const spill = await dialog.evaluate((el) => el.scrollWidth - el.clientWidth);
	expect(spill).toBeLessThanOrEqual(1);
	await assertNoHorizontalScroll(page);
}

async function seedCompletedRun(
	page: Page,
	csrf: string,
	instanceId: string,
	suffix: string
): Promise<string> {
	const fakeworkerBin = requireEnv('SNATCHARR_E2E_FAKEWORKER');
	const grpcAddr = process.env.SNATCHARR_E2E_GRPC || '127.0.0.1:19090';
	const workerToken = process.env.SNATCHARR_E2E_WORKER_TOKEN || 'smoke-worker-token';

	const triggerRes = await page.request.post(`/api/v1/instances/${instanceId}/runs`, {
		headers: { 'X-CSRF-Token': csrf },
		data: { kind: 'missing' }
	});
	expect(triggerRes.ok()).toBeTruthy();
	const run = (await triggerRes.json()) as { id: string };

	await execFileAsync(fakeworkerBin, [
		'-grpc',
		grpcAddr,
		'-worker-id',
		`dialog-worker-884-${suffix}`,
		'-instance',
		instanceId,
		'-outcome',
		'done',
		'-count',
		'1',
		'-timeout',
		'10s',
		'-token',
		workerToken
	]);
	return run.id;
}

interface ApiEvent {
	type: string;
	run_id?: string;
}

// The dashboard lists events of every instance, so the row is found by the type of an event
// of the seeded run; the dialog then names whichever instance that row belongs to.
async function runEventType(page: Page, runId: string): Promise<string> {
	const res = await page.request.get('/api/v1/events');
	expect(res.ok()).toBeTruthy();
	const mine = ((await res.json()) as ApiEvent[]).find((e) => e.run_id === runId);
	expect(mine, 'an event of the seeded run').toBeDefined();
	return mine?.type ?? '';
}

async function instanceNameById(page: Page, id: string): Promise<string> {
	const res = await page.request.get(`/api/v1/instances/${id}`);
	expect(res.ok()).toBeTruthy();
	return ((await res.json()) as { name: string }).name;
}

async function expectInstanceLink(page: Page, dialog: Locator): Promise<string> {
	const link = dialog.locator('a[href^="/instances/"]').first();
	await expect(link).toBeVisible();
	const id = ((await link.getAttribute('href')) ?? '').replace('/instances/', '');
	await expect(link).toHaveText(await instanceNameById(page, id));
	return id;
}

async function openEventThenRun(page: Page, eventType: string): Promise<void> {
	const dialog = page.getByRole('dialog');
	const row = page.locator(`[data-testid="dashboard-event"][data-event-type="${eventType}"]`);
	await row.first().click();
	await expectDialogFitsViewport(page, dialog);
	await expect(dialog.getByText(eventType, { exact: true })).toBeVisible();
	await expectInstanceLink(page, dialog);

	const runBtn = dialog.getByRole('button', { name: /^[0-9a-f-]{36}$/ });
	await expect(runBtn).toBeVisible();
	const shownRunId = (await runBtn.textContent())?.trim() ?? '';
	await runBtn.click();
	await expectDialogFitsViewport(page, dialog);
	await expect(dialog.locator('dd.font-mono')).toHaveText(shownRunId);
	await expectInstanceLink(page, dialog);
	await page.keyboard.press('Escape');
	if (await dialog.isVisible()) {
		await page.keyboard.press('Escape');
	}
	await expect(dialog).toBeHidden();
}

test('884: dashboard events and instance runs open detail dialogs that fit desktop and phone viewports and link to instances', async ({
	page
}) => {
	const fakearrUrl = requireEnv('SNATCHARR_E2E_FAKEARR');
	const suffix = crypto.randomUUID().slice(0, 8);
	const csrf = await getCsrfToken(page);
	let instanceId = '';

	try {
		const instRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrf },
			data: {
				kind: 'sonarr',
				name: `e2e-dialog-sonarr-${suffix}`,
				base_url: `${fakearrUrl}/e2e-884-${suffix}`,
				api_key: 'fake-api-key-0123456789',
				hourly_cap: 20,
				enabled: true
			}
		});
		expect(instRes.ok()).toBeTruthy();
		instanceId = ((await instRes.json()) as { id: string }).id;
		const runId = await seedCompletedRun(page, csrf, instanceId, suffix);
		const eventType = await runEventType(page, runId);

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/');
		await openEventThenRun(page, eventType);

		await page.setViewportSize(VIEWPORTS.phone);
		await page.goto('/');
		await openEventThenRun(page, eventType);

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto(`/instances/${instanceId}/activity`);
		const runsSection = page.locator('section[aria-labelledby="runs-title"]');
		await runsSection.locator('tbody tr td button').first().click();
		const dialog = page.getByRole('dialog');
		await expectDialogFitsViewport(page, dialog);
		await expect(dialog.locator('dd.font-mono')).toHaveText(runId);
	} finally {
		if (instanceId) {
			await page.request.delete(`/api/v1/instances/${instanceId}`, {
				headers: { 'X-CSRF-Token': csrf }
			});
		}
	}
});
