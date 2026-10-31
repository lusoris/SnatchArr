// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

import { expect, test, type Page } from '@playwright/test';

const execFileAsync = promisify(execFile);

const LAST_RUN = 'snatcharr_instance_last_run_finished_timestamp_seconds';
const INTERVAL = 'snatcharr_instance_run_interval_seconds';
const CYCLE_INTERVAL_S = 1234;

function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) {
		throw new Error(`missing environment variable ${name}`);
	}
	return value;
}

async function getCsrfToken(page: Page): Promise<string> {
	const res = await page.request.get('/api/v1/auth/session');
	expect(res.ok()).toBeTruthy();
	return ((await res.json()) as { csrf_token: string }).csrf_token;
}

// Reads one series from the API's own /metrics; the preview server proxies only /api.
async function seriesValue(page: Page, metric: string, labels: string): Promise<number | null> {
	const res = await page.request.get(`${requireEnv('SNATCHARR_E2E_API')}/metrics`);
	expect(res.status()).toBe(200);
	const prefix = `${metric}{${labels}} `;
	const line = (await res.text()).split('\n').find((row) => row.startsWith(prefix));
	return line === undefined ? null : Number(line.slice(prefix.length));
}

async function driveRun(
	page: Page,
	csrfToken: string,
	instanceID: string,
	outcome: 'done' | 'failed'
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
		'gauge-worker',
		'-instance',
		instanceID,
		'-outcome',
		outcome,
		'-count',
		'1',
		'-timeout',
		'10s',
		'-token',
		process.env.SNATCHARR_E2E_WORKER_TOKEN || 'smoke-worker-token'
	]);
}

test('instance run gauges follow the database and leave with the instance', async ({ page }) => {
	// The state loop ticks every 30 s and the planner writes a missing policy within 60 s.
	test.setTimeout(240_000);
	const poll = { timeout: 100_000, intervals: [2_000] };
	const csrfToken = await getCsrfToken(page);
	const suffix = crypto.randomUUID().slice(0, 8);
	const name = `e2e-gauge-${suffix}`;
	const instanceLabel = `arr_instance="${name}"`;
	const headers = { 'X-CSRF-Token': csrfToken };

	const created = await page.request.post('/api/v1/instances', {
		headers,
		data: {
			kind: 'sonarr',
			name,
			base_url: `${requireEnv('SNATCHARR_E2E_FAKEARR')}/e2e-gauge-${suffix}`,
			api_key: 'fake-api-key-0123456789',
			enabled: true
		}
	});
	expect(created.status()).toBe(201);
	const instanceID = ((await created.json()) as { id: string }).id;

	try {
		// Boundary: no run has finished, so no last-run series exists.
		expect(await seriesValue(page, LAST_RUN, `${instanceLabel},outcome="done"`)).toBeNull();

		const policyPath = `/api/v1/instances/${instanceID}/policy`;
		const policyRes = await page.request.get(policyPath);
		expect(policyRes.ok()).toBeTruthy();
		const policy = (await policyRes.json()) as Record<string, unknown>;
		const saved = await page.request.put(policyPath, {
			headers,
			data: { ...policy, cycle_interval_s: CYCLE_INTERVAL_S }
		});
		expect(saved.ok()).toBeTruthy();

		const before = Math.floor(Date.now() / 1000) - 5;
		await driveRun(page, csrfToken, instanceID, 'done');
		await driveRun(page, csrfToken, instanceID, 'failed');

		// Positive: one series per outcome, and the policy's interval.
		for (const outcome of ['done', 'failed']) {
			await expect
				.poll(() => seriesValue(page, LAST_RUN, `${instanceLabel},outcome="${outcome}"`), poll)
				.toBeGreaterThanOrEqual(before);
		}
		await expect
			.poll(() => seriesValue(page, INTERVAL, instanceLabel), poll)
			.toBe(CYCLE_INTERVAL_S);

		// Negative: outcome values stay the terminal statuses.
		expect(await seriesValue(page, LAST_RUN, `${instanceLabel},outcome="queued"`)).toBeNull();
	} finally {
		const removed = await page.request.delete(`/api/v1/instances/${instanceID}`, { headers });
		expect(removed.ok()).toBeTruthy();
	}

	// Negative: the series leave with the instance.
	await expect.poll(() => seriesValue(page, INTERVAL, instanceLabel), poll).toBeNull();
	expect(await seriesValue(page, LAST_RUN, `${instanceLabel},outcome="done"`)).toBeNull();
});
