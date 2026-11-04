// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #974 #971

import http from 'node:http';
import { expect, test, type Page } from '@playwright/test';
import { listenStub, type StubHandle } from './feature-helpers.js';
import { createRow, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

const REQUEST_TIMEOUT_MS = 10_000;
const MAX_RECORDED = 200;

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

// SABnzbd read calls use GET or HEAD with an empty name parameter and one of these
// modes (goenvoy downloadclient/sabnzbd/v2@v2.1.0). Everything else is a write.
const SAB_READ_MODES = new Set([
	'version',
	'queue',
	'history',
	'server_stats',
	'warnings',
	'fullstatus'
]);

function isSabRead(method: string, mode: string, name: string): boolean {
	return (method === 'GET' || method === 'HEAD') && name === '' && SAB_READ_MODES.has(mode);
}

function startSabStub(): Promise<SabStubHandle> {
	const requests: RecordedRequest[] = [];
	const server = http.createServer((req, res) => {
		const method = req.method ?? '';
		const reqUrl = req.url ?? '';
		const parsed = new URL(reqUrl, 'http://127.0.0.1');
		const mode = parsed.searchParams.get('mode') ?? '';
		const name = parsed.searchParams.get('name') ?? '';

		const isWrite = !isSabRead(method, mode, name);

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

// expectReadOnly asserts the stub saw at least one request and that every request was a read.
function expectReadOnly(stub: SabStubHandle): void {
	expect(stub.requests.length).toBeGreaterThanOrEqual(1);
	expect(stub.requests.length).toBeLessThanOrEqual(MAX_RECORDED);
	for (const req of stub.requests.slice(0, MAX_RECORDED)) {
		expect(req.method).toBe('GET');
		expect(req.isWrite).toBe(false);
	}
}

// createSab registers a SABnzbd download client that points at a stub and returns its id.
async function createSab(
	page: Page,
	csrf: string,
	prefix: string,
	url: string,
	enabled: boolean
): Promise<string> {
	return createRow(page, csrf, 'download-clients', {
		kind: 'sabnzbd',
		name: `${prefix}-${uniqueSuffix()}`,
		base_url: url,
		secret: 'fake-sab-api-key',
		enabled
	});
}

// PLAN_TEST_TIMEOUT_MS covers setup, the 45 s waitForPlan poll, the assertions and cleanup.
const PLAN_TEST_TIMEOUT_MS = 120_000;

// waitForPlan polls the client's priority plan until a reconcile tick has served it. The
// leader reconciler observes every 15 s in this suite (tools/e2e/web-e2e-api.sh).
async function waitForPlan(page: Page, clientId: string): Promise<DownloadClientPriorityPlan> {
	const planUrl = `/api/v1/download-clients/${clientId}/priority-plan`;
	await expect
		.poll(async () => (await page.request.get(planUrl, { timeout: REQUEST_TIMEOUT_MS })).status(), {
			timeout: 45_000,
			intervals: [1_000]
		})
		.toBe(200);
	const res = await page.request.get(planUrl, { timeout: REQUEST_TIMEOUT_MS });
	return (await res.json()) as DownloadClientPriorityPlan;
}

// expectProblem404 asserts that a GET of url answers 404 with an RFC 9457 problem body.
async function expectProblem404(page: Page, url: string): Promise<void> {
	const res = await page.request.get(url, { timeout: REQUEST_TIMEOUT_MS });
	expect(res.status()).toBe(404);
	expect(res.headers()['content-type']).toContain('application/problem+json');
	expect(((await res.json()) as { status: number }).status).toBe(404);
}

// cleanup deletes the created client rows, then closes every stub even if the delete fails.
async function cleanup(
	page: Page,
	csrf: string,
	ids: Array<string | undefined>,
	stubs: SabStubHandle[]
): Promise<void> {
	try {
		const toDelete = ids.filter((id): id is string => Boolean(id));
		if (toDelete.length > 0) {
			await deleteRows(page, csrf, 'download-clients', toDelete);
		}
	} finally {
		await Promise.allSettled(stubs.map((s) => s.close()));
	}
}

test('974: positive - download client priority-plan read returns plan or pending 404, and writes nothing', async ({
	page
}) => {
	// waitForPlan polls for up to 45 s; the default 30 s test timeout would end the test first.
	test.setTimeout(PLAN_TEST_TIMEOUT_MS);
	const stub = await startSabStub();
	let csrf = '';
	let clientId: string | undefined;
	try {
		csrf = await signIn(page);
		clientId = await createSab(page, csrf, 'e2e-sab-974', stub.url, true);

		// Test/probe endpoint contacts client in read-only mode to verify reachability.
		const probeRes = await page.request.post(`/api/v1/download-clients/${clientId}/test`, {
			headers: { 'X-CSRF-Token': csrf },
			timeout: REQUEST_TIMEOUT_MS
		});
		expect(probeRes.status()).toBe(200);
		expect(((await probeRes.json()) as { reachable: boolean }).reachable).toBe(true);

		// A client created through the API has no category map, so attribution counts both
		// queued stub items as unattributed and every action list stays empty.
		const plan = await waitForPlan(page, clientId);
		expect(plan.clientId).toBe(clientId);
		expect(plan.mode).toBe('observe');
		expect(typeof plan.observedAt).toBe('string');
		expect(plan.queueingDisabled).toBe(false);
		expect(plan.unattributed).toBe(2);
		expect(plan.priority).toEqual([]);
		expect(plan.order).toEqual([]);
		expect(plan.pause).toEqual([]);
		expect(plan.resume).toEqual([]);

		// Read-only invariant: every request hitting the stub was a GET, with zero write calls.
		expectReadOnly(stub);
	} finally {
		await cleanup(page, csrf, [clientId], [stub]);
	}
});

test('974: negative - unknown or malformed client id returns problem+json', async ({ page }) => {
	await expectProblem404(
		page,
		'/api/v1/download-clients/00000000-0000-0000-0000-000000000000/priority-plan'
	);

	const malformedRes = await page.request.get(
		'/api/v1/download-clients/not-a-valid-uuid/priority-plan',
		{ timeout: REQUEST_TIMEOUT_MS }
	);
	expect([400, 404, 422]).toContain(malformedRes.status());
	expect(malformedRes.headers()['content-type']).toContain('application/problem+json');
	const malformedProblem = (await malformedRes.json()) as { status: number };
	expect(malformedProblem.status).toBe(malformedRes.status());
});

test('974: boundary - disabled client returns 404 pending/unobserved shape without contacting stub writes', async ({
	page
}) => {
	// waitForPlan polls for up to 45 s; the default 30 s test timeout would end the test first.
	test.setTimeout(PLAN_TEST_TIMEOUT_MS);
	const stubA = await startSabStub();
	const stubB = await startSabStub();
	let csrf = '';
	let clientAId: string | undefined;
	let clientBId: string | undefined;
	try {
		csrf = await signIn(page);
		// The disabled client A exists before the enabled client B, so the tick that first
		// serves B's plan necessarily ran with A in the table.
		clientAId = await createSab(page, csrf, 'e2e-sab-dis', stubA.url, false);
		clientBId = await createSab(page, csrf, 'e2e-sab-en', stubB.url, true);

		await waitForPlan(page, clientBId);

		// A disabled client is never contacted and never observed: zero requests, plan 404.
		expect(stubA.requests.length).toBe(0);
		await expectProblem404(page, `/api/v1/download-clients/${clientAId}/priority-plan`);

		// Stub B was contacted via read-only GET requests.
		expectReadOnly(stubB);
	} finally {
		await cleanup(page, csrf, [clientAId, clientBId], [stubA, stubB]);
	}
});
