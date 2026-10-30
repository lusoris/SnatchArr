// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #882

import { expect, test } from '@playwright/test';

const validUrls = [
	'/api/v1/events',
	'/api/v1/events?type=search_dispatched',
	'/api/v1/events?type=run_queued',
	'/api/v1/events?before_id=1',
	'/api/v1/logs',
	'/api/v1/logs?source=api',
	'/api/v1/logs?source=worker',
	'/api/v1/logs?source=worker:e2e-worker-1'
];

const invalidUrls = [
	'/api/v1/events?type=nope',
	'/api/v1/events?type=EVENT_TYPE_SEARCH_DISPATCHED',
	'/api/v1/events?type=',
	'/api/v1/events?before_id=0',
	'/api/v1/events?before_id=-5',
	'/api/v1/logs?source=nope',
	'/api/v1/logs?source=',
	'/api/v1/logs?source=worker:',
	'/api/v1/logs?source=worker:a%20b',
	'/api/v1/logs?source=worker:a:b',
	'/api/v1/logs?level=nope'
];

test('882: list filters that can never match return 400 problem+json while valid filters return 200', async ({
	page
}) => {
	for (const url of validUrls) {
		const res = await page.request.get(url);
		expect(res.status(), `expected 200 for ${url}`).toBe(200);
	}

	for (const url of invalidUrls) {
		const res = await page.request.get(url);
		expect(res.status(), `expected 400 for ${url}`).toBe(400);
		expect(res.headers()['content-type']).toContain('application/problem+json');
		const problem = (await res.json()) as { status: number; detail?: string };
		expect(problem.status).toBe(400);
	}

	await page.goto('/activity?type=nope');
	await expect(page.locator('[role="alert"]').first()).toBeVisible();
});
