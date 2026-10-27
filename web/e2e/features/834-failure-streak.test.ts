// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #834

import { execFile } from 'node:child_process';
import http from 'node:http';
import type net from 'node:net';
import { promisify } from 'node:util';
import { expect, test, type Page } from '@playwright/test';

const execFileAsync = promisify(execFile);

// The feature run sets these; a missing one is a broken harness, not a reason to skip.
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

interface ReceivedWebhook {
	headers: http.IncomingHttpHeaders;
	body: string;
}

async function startWebhookSink(): Promise<{
	url: string;
	deliveries: ReceivedWebhook[];
	close: () => Promise<void>;
}> {
	const deliveries: ReceivedWebhook[] = [];
	const server = http.createServer((req, res) => {
		let body = '';
		req.on('data', (chunk) => {
			body += chunk;
		});
		req.on('end', () => {
			deliveries.push({ headers: req.headers, body });
			res.writeHead(200, { 'Content-Type': 'application/json' });
			res.end(JSON.stringify({ ok: true }));
		});
	});
	await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
	const port = (server.address() as net.AddressInfo).port;
	return {
		url: `http://127.0.0.1:${port}/webhook`,
		deliveries,
		close: () =>
			new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())))
	};
}

async function driveRun(
	page: Page,
	csrfToken: string,
	instanceID: string,
	fakeworkerBin: string,
	grpcAddr: string,
	outcome: 'failed' | 'done',
	workerToken: string
): Promise<void> {
	const triggerRes = await page.request.post(`/api/v1/instances/${instanceID}/runs`, {
		headers: { 'X-CSRF-Token': csrfToken },
		data: { kind: 'missing' }
	});
	expect(triggerRes.ok()).toBeTruthy();

	await execFileAsync(fakeworkerBin, [
		'-grpc',
		grpcAddr,
		'-worker-id',
		'streak-worker-834',
		'-instance',
		instanceID,
		'-outcome',
		outcome,
		'-count',
		'1',
		'-timeout',
		'10s',
		'-token',
		workerToken
	]);
}

test('positive: failure streak fires at threshold, deduplicates on higher streak, resets on success', async ({
	page
}) => {
	const fakeworkerBin = requireEnv('SNATCHARR_E2E_FAKEWORKER');
	const fakearrUrl = requireEnv('SNATCHARR_E2E_FAKEARR');
	const suffix = crypto.randomUUID().slice(0, 8);

	const grpcAddr = process.env.SNATCHARR_E2E_GRPC || '127.0.0.1:19090';
	const workerToken = process.env.SNATCHARR_E2E_WORKER_TOKEN || 'smoke-worker-token';
	const csrfToken = await getCsrfToken(page);
	const sink = await startWebhookSink();

	let instanceID = '';
	let channelID = '';
	let ruleID = '';

	try {
		// 1. Create an instance for this test
		const instRes = await page.request.post('/api/v1/instances', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				kind: 'sonarr',
				name: `e2e-streak-sonarr-${suffix}`,
				base_url: `${fakearrUrl}/e2e-834-${suffix}`,
				api_key: 'fake-api-key-0123456789',
				enabled: true
			}
		});
		expect(instRes.status()).toBe(201);
		const instData = (await instRes.json()) as { id: string };
		instanceID = instData.id;

		// 2. Create a webhook notification channel pointing to the test sink
		const chanRes = await page.request.post('/api/v1/notifications/channels', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'streak-webhook-channel',
				kind: 'webhook',
				enabled: true,
				config: {
					url: sink.url,
					allow_private_network: true,
					allow_insecure_http: true
				}
			}
		});
		expect(chanRes.status()).toBe(201);
		const chanData = (await chanRes.json()) as { id: string };
		channelID = chanData.id;

		// 3. Create a failure streak rule with threshold 2
		const ruleRes = await page.request.post('/api/v1/notifications/rules', {
			headers: { 'X-CSRF-Token': csrfToken },
			data: {
				name: 'streak-threshold-2',
				event_kind: 'run_failure_streak',
				threshold: 2,
				enabled: true,
				channel_ids: [channelID]
			}
		});
		expect(ruleRes.status()).toBe(201);
		const ruleData = (await ruleRes.json()) as { id: string };
		ruleID = ruleData.id;

		// Run 1: first failure -> streak = 1 != threshold 2 -> no streak delivery
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'failed', workerToken);
		await page.waitForTimeout(500);
		expect(sink.deliveries.length).toBe(0);

		// Run 2: second failure -> streak = 2 == threshold 2 -> exactly one delivery
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'failed', workerToken);
		await expect.poll(() => sink.deliveries.length, { timeout: 5000 }).toBe(1);

		// Run 3: third failure -> streak = 3 != threshold 2 -> still one delivery (streak == threshold, not >=)
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'failed', workerToken);
		await page.waitForTimeout(500);
		expect(sink.deliveries.length).toBe(1);

		// Run 4: negative: done run resets streak
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'done', workerToken);
		await page.waitForTimeout(500);
		expect(sink.deliveries.length).toBe(1);

		// Run 5: failure after reset -> streak = 1 -> no new delivery
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'failed', workerToken);
		await page.waitForTimeout(500);
		expect(sink.deliveries.length).toBe(1);

		// Run 6: second failure after reset -> streak = 2 == threshold 2 -> second delivery arrives
		await driveRun(page, csrfToken, instanceID, fakeworkerBin, grpcAddr, 'failed', workerToken);
		await expect.poll(() => sink.deliveries.length, { timeout: 5000 }).toBe(2);
	} finally {
		if (ruleID) {
			await page.request.delete(`/api/v1/notifications/rules/${ruleID}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		if (channelID) {
			await page.request.delete(`/api/v1/notifications/channels/${channelID}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		if (instanceID) {
			await page.request.delete(`/api/v1/instances/${instanceID}`, {
				headers: { 'X-CSRF-Token': csrfToken }
			});
		}
		await sink.close();
	}
});

test('negative: rule creation refuses a zero threshold and empty channel_ids with a problem', async ({
	page
}) => {
	const csrfToken = await getCsrfToken(page);

	// Zero threshold breaks the schema minimum: request validation answers 422
	const zeroThresholdRes = await page.request.post('/api/v1/notifications/rules', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			name: 'invalid-zero-streak',
			event_kind: 'run_failure_streak',
			threshold: 0,
			channel_ids: ['00000000-0000-0000-0000-000000000001']
		}
	});
	expect(zeroThresholdRes.status()).toBe(422);
	expect(zeroThresholdRes.headers()['content-type']).toContain('application/problem+json');

	// Empty channel_ids passes the schema; the service refuses it as invalid (422)
	const emptyChannelsRes = await page.request.post('/api/v1/notifications/rules', {
		headers: { 'X-CSRF-Token': csrfToken },
		data: {
			name: 'invalid-empty-channels',
			event_kind: 'run_failure_streak',
			threshold: 2,
			channel_ids: []
		}
	});
	expect(emptyChannelsRes.status()).toBe(422);
	expect(emptyChannelsRes.headers()['content-type']).toContain('application/problem+json');
});

test('boundary: non-existent rule id operations return 404 problem', async ({ page }) => {
	const csrfToken = await getCsrfToken(page);
	const dummyId = '00000000-0000-0000-0000-000000000000';

	const getRes = await page.request.get(`/api/v1/notifications/rules/${dummyId}`);
	expect(getRes.status()).toBe(404);
	expect(getRes.headers()['content-type']).toContain('application/problem+json');

	const delRes = await page.request.delete(`/api/v1/notifications/rules/${dummyId}`, {
		headers: { 'X-CSRF-Token': csrfToken }
	});
	expect(delRes.status()).toBe(404);
	expect(delRes.headers()['content-type']).toContain('application/problem+json');
});
