// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Rendered layout of every authenticated route at desktop and phone width (API mode). With
// SNATCHARR_SERVED_URL set, the served-layout project runs the same checks against a deployed
// site and stores one full-page screenshot per route and viewport.
import fs from 'node:fs';
import path from 'node:path';
import { test } from '@playwright/test';
import { assertRenderedLayout, AUTH_ROUTES, VIEWPORTS } from './layout.js';

const served = !!process.env.SNATCHARR_SERVED_URL;
// Screenshots: always against a served site, locally on request (SNATCHARR_LAYOUT_SHOTS=1).
const shots = served || !!process.env.SNATCHARR_LAYOUT_SHOTS;

function seededInstanceId(): string {
	const seedFile = path.resolve(import.meta.dirname, '.auth/seed.json');
	let data: { instanceId?: string };
	try {
		data = JSON.parse(fs.readFileSync(seedFile, 'utf-8')) as { instanceId?: string };
	} catch (err) {
		throw new Error(
			`unreadable seed file ${seedFile}: ${err instanceof Error ? err.message : String(err)}`,
			{ cause: err }
		);
	}
	if (!data.instanceId) {
		throw new Error(`seed file ${seedFile} missing instanceId field`);
	}
	return data.instanceId;
}

function instanceId(): string {
	const fromEnv = process.env.SNATCHARR_SERVED_INSTANCE_ID;
	if (served) {
		if (!fromEnv) throw new Error('SNATCHARR_SERVED_INSTANCE_ID is required with a served URL');
		return fromEnv;
	}
	return seededInstanceId();
}

// History rows as the served page showed them, plus one long Detail: the row that used to size
// its own columns and push the others out of line.
const historyEvents = [
	{
		id: 3,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'debug',
		type: 'candidates_filtered',
		detail:
			'43 candidate(s) after filters and afterglow; 2 skipped as queued or grabbed within 24 h; 0 skipped as not yet available; 13 requested via Seerr'
	},
	{
		id: 2,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'warn',
		type: 'stamina_exhausted',
		title: 'Stamina exhausted',
		detail: 'hourly cap (15) exhausted'
	},
	{
		id: 1,
		instance_id: '00000000-0000-0000-0000-000000000001',
		ts: '2026-10-21T06:19:00Z',
		level: 'info',
		type: 'search_dispatched',
		title: 'S11E04 The Lost Art of Forehead Sweat',
		detail: 'command 703361'
	}
];

for (const [viewportName, viewport] of Object.entries(VIEWPORTS)) {
	for (const pattern of AUTH_ROUTES) {
		test(`authenticated: layout ${pattern} ${viewportName}`, async ({ page }, testInfo) => {
			await page.setViewportSize(viewport);
			if (pattern === '/history' && !served) {
				await page.route('**/api/v1/events?**', (route) =>
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(historyEvents)
					})
				);
			}
			const route = pattern.replace('[id]', instanceId());
			await page.goto(route);
			await assertRenderedLayout(page, pattern);
			if (shots) {
				const name = `${pattern.replace(/[/[\]]+/g, '-').replace(/^-|-$/g, '') || 'root'}-${viewportName}.png`;
				await page.screenshot({ path: testInfo.outputPath(name), fullPage: true });
			}
		});
	}
}
