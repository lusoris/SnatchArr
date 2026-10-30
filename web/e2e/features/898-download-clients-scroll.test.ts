// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #898 #917
import { expect, test, type Page } from '@playwright/test';
import { VIEWPORTS, assertScrollBudget, assertTableFits } from '../layout.js';
import { createRow, deleteRows, signIn, uniqueSuffix } from './layout-fixtures.js';

const ROUTE = '/connections/download-clients';
// A name far longer than the 88px the name cell may take, so the cell truncates it.
const LONG_NAME = 'e2e-dlclient-898-transmission-with-a-long-name';

async function seedClient(page: Page, csrf: string, name: string): Promise<string> {
	return createRow(page, csrf, 'download-clients', {
		name,
		kind: 'transmission',
		base_url: `http://127.0.0.1:9091/${name}`,
		enabled: true
	});
}

async function boxOf(page: Page, selector: string): Promise<{ x: number; y: number }> {
	const box = await page.locator(selector).boundingBox();
	expect(box, `${selector} has no box`).not.toBeNull();
	return { x: box!.x, y: box!.y };
}

// Height of the table rows that other tests left in the shared database: the scroll budget
// measures the page, not the rows of other tests, so they are added to the allowance.
async function foreignRowsHeight(page: Page, ownName: string): Promise<number> {
	return page
		.locator('tbody > tr')
		.evaluateAll(
			(rows, own) =>
				rows
					.filter((row) => !(row.textContent ?? '').includes(own))
					.reduce((sum, row) => sum + row.getBoundingClientRect().height, 0),
			ownName
		);
}

test('898: the download client table fits its column at 1280 and truncates a long name', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `${LONG_NAME}-${uniqueSuffix()}`;
	const id = await seedClient(page, csrf, name);
	try {
		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto(ROUTE);
		const heading = page.locator(`h2[title="${name}"]`);
		await expect(heading).toBeVisible();

		// The name cell keeps 88px for the name beside the logo; the full name stays in the title.
		const width = (await heading.boundingBox())!.width;
		expect(width, `name cell takes ${width}px, more than the 88px it may`).toBeLessThanOrEqual(88);
		await expect(heading).toHaveText(name);
		expect(
			await heading.evaluate((el) => el.scrollWidth > el.clientWidth),
			'a long name is cut, not wrapped over the table'
		).toBe(true);

		await assertTableFits(page, 'table');
	} finally {
		await deleteRows(page, csrf, 'download-clients', [id]);
	}
});

test('917: the download clients page keeps its scroll budget with the add form in two columns', async ({
	page
}) => {
	const csrf = await signIn(page);
	const name = `e2e-dlclient-917-${uniqueSuffix()}`;
	const id = await seedClient(page, csrf, name);
	try {
		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto(ROUTE);
		await expect(page.locator('#dlclient-name')).toBeVisible();

		// From md the add form puts its fields in two columns: kind and name share one row.
		const kind = await boxOf(page, '#dlclient-kind');
		const field = await boxOf(page, '#dlclient-name');
		expect(Math.abs(kind.y - field.y), 'kind and name sit in one row').toBeLessThanOrEqual(4);
		expect(field.x - kind.x, 'name sits in the second column').toBeGreaterThan(100);

		await expect(page.locator('tbody > tr').filter({ hasText: name })).toBeVisible();
		const rows = await foreignRowsHeight(page, name);
		await assertScrollBudget(page, ROUTE, 1.25 * VIEWPORTS.desktop.height + rows);
	} finally {
		await deleteRows(page, csrf, 'download-clients', [id]);
	}
});
