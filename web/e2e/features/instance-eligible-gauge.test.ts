// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

import { expect, test, type Page } from '@playwright/test';

const execFileAsync = promisify(execFile);
const ELIGIBLE = 'snatcharr_instance_eligible_items';

function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`missing environment variable ${name}`);
	}
	return value;
}

async function seriesValue(page: Page, name: string): Promise<number | null> {
	const res = await page.request.get(`${requireEnv('SNATCHARR_E2E_API')}/metrics`);
	expect(res.status()).toBe(200);
	const prefix = `${ELIGIBLE}{arr_instance="${name}",kind="missing"} `;
	const line = (await res.text()).split('\n').find((row) => row.startsWith(prefix));
	return line === undefined ? null : Number(line.slice(prefix.length));
}

async function driveRun(
	page: Page,
	csrfToken: string,
	instanceID: string,
	eligible: number
): Promise<void> {
	const trigger = await page.request.post(`/api/v1/instances/${instanceID}/runs`, {
		headers: { 'X-CSRF-Token': csrfToken },
		data: { kind: 'missing' }
	});
	expect(trigger.ok()).toBeTruthy();
	await execFileAsync(requireEnv('SNATCHARR_E2E_FAKEWORKER'), [
		'-grpc',
		process.env.SNATCHARR_E2E_GRPC || '127.0.0.1:19090',
		'-worker-id',
		'eligible-gauge-worker',
		'-instance',
		instanceID,
		'-outcome',
		'done',
		'-eligible',
		String(eligible),
		'-count',
		'1',
		'-timeout',
		'10s',
		'-token',
		process.env.SNATCHARR_E2E_WORKER_TOKEN || 'smoke-worker-token'
	]);
}

test('eligible pool follows completed runs and leaves with the instance', async ({ page }) => {
	test.setTimeout(240_000);
	const poll = { timeout: 100_000, intervals: [2_000] };
	const session = await page.request.get('/api/v1/auth/session');
	expect(session.ok()).toBeTruthy();
	const csrfToken = ((await session.json()) as { csrf_token: string }).csrf_token;
	const headers = { 'X-CSRF-Token': csrfToken };
	const suffix = crypto.randomUUID().slice(0, 8);
	const name = `e2e-eligible-${suffix}`;
	const created = await page.request.post('/api/v1/instances', {
		headers,
		data: {
			kind: 'sonarr',
			name,
			base_url: `${requireEnv('SNATCHARR_E2E_FAKEARR')}/e2e-eligible-${suffix}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		}
	});
	expect(created.status()).toBe(201);
	const instanceID = ((await created.json()) as { id: string }).id;

	try {
		// No reported pool yet: absence must stay distinguishable from zero.
		expect(await seriesValue(page, name)).toBeNull();
		await driveRun(page, csrfToken, instanceID, 7);
		await expect.poll(() => seriesValue(page, name), poll).toBe(7);
		await driveRun(page, csrfToken, instanceID, 0);
		await expect.poll(() => seriesValue(page, name), poll).toBe(0);
	} finally {
		const removed = await page.request.delete(`/api/v1/instances/${instanceID}`, { headers });
		expect(removed.ok()).toBeTruthy();
	}
	await expect.poll(() => seriesValue(page, name), poll).toBeNull();
});
