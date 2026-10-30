// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #895
import { test, type Page } from '@playwright/test';
import { VIEWPORTS, assertRenderedLayout, collectLists } from '../layout.js';
import { createInstance, createRow, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

// Routes whose tables stack into labelled cards on a phone: the dashboard fleet table, the
// download client table and the schedule table. assertRenderedLayout fails a stacked table
// whose header shows, whose card clips its fields or runs into the next card.
const STACKED_ROUTES = ['/', '/connections/download-clients', '/automation'] as const;

async function seedStackedRows(
	page: Page,
	csrf: string,
	name: string,
	ids: { instances: string[]; clients: string[]; schedules: string[] }
): Promise<void> {
	ids.instances.push(await createInstance(page, csrf, name));
	ids.clients.push(
		await createRow(page, csrf, 'download-clients', {
			name,
			kind: 'transmission',
			base_url: `http://127.0.0.1:9091/${name}`,
			enabled: true
		})
	);
	ids.schedules.push(
		await createRow(page, csrf, 'schedules', {
			name,
			days: [1],
			start: '03:00',
			end: '03:05',
			tz: 'UTC',
			action: 'cap_override',
			cap_value: 500,
			enabled: true
		})
	);
}

test('895: stacked tables on a phone hide their header and keep every card readable', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `e2e-895-${uniqueSuffix()}`;
	const ids = { instances: [] as string[], clients: [] as string[], schedules: [] as string[] };
	try {
		await seedStackedRows(page, csrf, name, ids);
		await page.setViewportSize(VIEWPORTS.phone);
		for (const route of STACKED_ROUTES) {
			// Register before goto: the helper reads the lists the page receives.
			const lists = collectLists(page);
			await page.goto(route);
			await assertRenderedLayout(page, route, lists);
		}
	} finally {
		await deleteRows(page, csrf, 'schedules', ids.schedules);
		await deleteRows(page, csrf, 'download-clients', ids.clients);
		await deleteRows(page, csrf, 'instances', ids.instances);
	}
});
