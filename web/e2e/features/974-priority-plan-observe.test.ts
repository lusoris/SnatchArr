// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #974 #971

import http from 'node:http';
import { expect, test } from '@playwright/test';
import { listenStub, type StubHandle } from './feature-helpers.js';
import { createRow, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

interface RecordedRequest {
	method: string;
	url: string;
	isWrite: boolean;
}

interface SabStubHandle extends StubHandle {
	requests: RecordedRequest[];
}

interface PriorityMove {
	id: string;
	from: number;
	to: number;
}

interface DownloadClientPriorityPlan {
	clientId: string;
	mode: 'observe' | 'act';
	observedAt: string;
	queueingDisabled: boolean;
	unattributed: number;
	priority: PriorityMove[];
	order: string[];
	pause: string[];
	resume: string[];
}

function startSabStub(): Promise<SabStubHandle> {
	const requests: RecordedRequest[] = [];
	const server = http.createServer((req, res) => {
		const method = req.method ?? '';
		const reqUrl = req.url ?? '';
		const parsed = new URL(reqUrl, 'http://127.0.0.1');
		const mode = parsed.searchParams.get('mode') ?? '';
		const name = parsed.searchParams.get('name') ?? '';

		// SABnzbd mutations occur through POST or specific write actions (pause, resume, priority, delete).
		const isWrite =
			(method !== 'GET' && method !== 'HEAD') ||
			mode === 'pause' ||
			mode === 'resume' ||
			name === 'priority' ||
			name === 'delete';

		requests.push({ method, url: reqUrl, isWrite });

		res.writeHead(200, { 'Content-Type': 'application/json' });
		if (mode === 'version') {
			res.end(JSON.stringify({ version: '4.2.0' }));
		} else if (mode === 'queue') {
			res.end(
				JSON.stringify({
					queue: {
						slots: [
							{
								nzo_id: 'sab-slot-1',
								cat: 'movies',
								status: 'Queued',
								priority: 'Normal',
								filename: 'Fake.Movie.2026'
							},
							{
								nzo_id: 'sab-slot-2',
								cat: 'tv',
								status: 'Downloading',
								priority: 'High',
								filename: 'Fake.Episode.S01E01'
							}
						],
						paused: false,
						speed: '0',
						kbpersec: '0'
					}
				})
			);
		} else {
			res.end(JSON.stringify({}));
		}
	});

	return listenStub(server).then((handle) => ({
		...handle,
		requests
	}));
}

test('974: positive - download client priority-plan read returns plan or pending 404, and writes nothing', async ({
	page
}) => {
	const stub = await startSabStub();
	const csrf = await signIn(page);
	const name = `e2e-sab-974-${uniqueSuffix()}`;

	const clientId = await createRow(page, csrf, 'download-clients', {
		kind: 'sabnzbd',
		name,
		base_url: stub.url,
		secret: 'fake-sab-api-key',
		enabled: true
	});

	try {
		// Test/probe endpoint contacts client in read-only mode to verify reachability.
		const probeRes = await page.request.post(`/api/v1/download-clients/${clientId}/test`, {
			headers: { 'X-CSRF-Token': csrf }
		});
		expect(probeRes.status()).toBe(200);
		const probeData = (await probeRes.json()) as { reachable: boolean };
		expect(probeData.reachable).toBe(true);

		// The leader reconciler observes every 15 s in this suite (tools/e2e/web-e2e-api.sh); the
		// plan exists only after a tick, so poll until it is served.
		const planUrl = `/api/v1/download-clients/${clientId}/priority-plan`;
		await expect
			.poll(async () => (await page.request.get(planUrl)).status(), {
				timeout: 45_000,
				intervals: [1_000]
			})
			.toBe(200);
		const plan = (await (await page.request.get(planUrl)).json()) as DownloadClientPriorityPlan;
		expect(plan.clientId).toBe(clientId);
		expect(plan.mode).toBe('observe');
		expect(typeof plan.observedAt).toBe('string');
		expect(typeof plan.queueingDisabled).toBe('boolean');
		expect(plan.unattributed).toBeGreaterThanOrEqual(0);
		expect(Array.isArray(plan.priority)).toBe(true);
		expect(Array.isArray(plan.order)).toBe(true);
		expect(Array.isArray(plan.pause)).toBe(true);
		expect(Array.isArray(plan.resume)).toBe(true);

		// Read-only invariant: every request hitting the stub was a GET, with zero write calls.
		expect(stub.requests.length).toBeGreaterThanOrEqual(1);
		for (const req of stub.requests) {
			expect(req.method).toBe('GET');
			expect(req.isWrite).toBe(false);
		}
	} finally {
		await deleteRows(page, csrf, 'download-clients', [clientId]);
		await stub.close();
	}
});

test('974: negative - unknown or malformed client id returns problem+json', async ({ page }) => {
	const unknownId = '00000000-0000-0000-0000-000000000000';
	const unknownRes = await page.request.get(`/api/v1/download-clients/${unknownId}/priority-plan`);
	expect(unknownRes.status()).toBe(404);
	expect(unknownRes.headers()['content-type']).toContain('application/problem+json');
	const unknownProblem = (await unknownRes.json()) as { status: number };
	expect(unknownProblem.status).toBe(404);

	const malformedRes = await page.request.get(
		'/api/v1/download-clients/not-a-valid-uuid/priority-plan'
	);
	expect([400, 404, 422]).toContain(malformedRes.status());
	expect(malformedRes.headers()['content-type']).toContain('application/problem+json');
	const malformedProblem = (await malformedRes.json()) as { status: number };
	expect([400, 404, 422]).toContain(malformedProblem.status);
});

test('974: boundary - disabled client returns 404 pending/unobserved shape without contacting stub writes', async ({
	page
}) => {
	const stub = await startSabStub();
	const csrf = await signIn(page);
	const name = `e2e-sab-dis-${uniqueSuffix()}`;

	const clientId = await createRow(page, csrf, 'download-clients', {
		kind: 'sabnzbd',
		name,
		base_url: stub.url,
		secret: 'fake-sab-api-key',
		enabled: false
	});

	try {
		const planRes = await page.request.get(`/api/v1/download-clients/${clientId}/priority-plan`);
		// A disabled client is never observed, so no plan is ever served for it.
		expect(planRes.status()).toBe(404);
		expect(planRes.headers()['content-type']).toContain('application/problem+json');
		const problem = (await planRes.json()) as { status: number };
		expect(problem.status).toBe(404);

		for (const req of stub.requests) {
			expect(req.method).toBe('GET');
			expect(req.isWrite).toBe(false);
		}
	} finally {
		await deleteRows(page, csrf, 'download-clients', [clientId]);
		await stub.close();
	}
});
