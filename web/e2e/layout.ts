// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Rendered-layout checks shared by e2e/layout.test.ts (CI, API mode) and the served-site run
// (SNATCHARR_SERVED_URL). They read the page as the browser laid it out: no horizontal page
// scroll, stylesheets applied, and every table column sharing one x and width between the
// header cell and the body cells of each row.
import { expect, type Page } from '@playwright/test';

export const VIEWPORTS = {
	desktop: { width: 1280, height: 800 },
	phone: { width: 390, height: 844 }
} as const;

export const AUTH_ROUTES = [
	'/',
	'/account',
	'/configarr',
	'/download-clients',
	'/history',
	'/instances',
	'/instances/[id]',
	'/instances/[id]/activity',
	'/instances/[id]/memory',
	'/instances/[id]/policy',
	'/instances/[id]/queue',
	'/live',
	'/logs',
	'/prowlarr',
	'/schedules',
	'/seerr',
	'/settings'
] as const;

// Allowed drift in CSS pixels between a header cell and the body cells below it.
const TOLERANCE_PX = 2;

interface Misaligned {
	table: number;
	row: number;
	column: number;
	header: { x: number; width: number };
	cell: { x: number; width: number };
}

// Every table and ARIA grid on the page: compare each body row whose cell count matches the
// header with the header cells, column by column. Rows that span columns are skipped.
function findMisalignedCells(tolerance: number): Misaligned[] {
	const box = (el: Element) => {
		const r = el.getBoundingClientRect();
		return { x: Math.round(r.left), width: Math.round(r.width) };
	};
	const tables = Array.from(document.querySelectorAll('table, [role="grid"]'));
	const found: Misaligned[] = [];
	tables.slice(0, 50).forEach((table, t) => {
		const isGrid = table.getAttribute('role') === 'grid';
		const headers = isGrid
			? Array.from(table.querySelectorAll('[role="columnheader"]'))
			: Array.from(table.querySelectorAll(':scope > thead > tr:first-child > th'));
		const rows = isGrid
			? Array.from(table.querySelectorAll('[role="row"]')).filter(
					(r) => r.querySelector('[role="gridcell"]') !== null
				)
			: Array.from(table.querySelectorAll(':scope > tbody > tr'));
		rows.slice(0, 500).forEach((row, r) => {
			const cells = isGrid
				? Array.from(row.querySelectorAll('[role="gridcell"]'))
				: Array.from(row.querySelectorAll(':scope > td, :scope > th'));
			if (cells.length !== headers.length) return;
			cells.forEach((cell, c) => {
				const h = box(headers[c]);
				const b = box(cell);
				if (Math.abs(h.x - b.x) > tolerance || Math.abs(h.width - b.width) > tolerance) {
					found.push({ table: t, row: r, column: c, header: h, cell: b });
				}
			});
		});
	});
	return found;
}

// Signals that the app stylesheet applied: the body paints a background and the sidebar
// links lose the user-agent underline.
function readStyleSignals() {
	const body = getComputedStyle(document.body);
	const link = document.querySelector('nav a');
	return {
		sheets: document.styleSheets.length,
		bodyBackground: body.backgroundColor,
		linkDecoration: link ? getComputedStyle(link).textDecorationLine : 'none'
	};
}

export async function assertRenderedLayout(page: Page, route: string): Promise<void> {
	await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();

	const styles = await page.evaluate(readStyleSignals);
	expect(styles.sheets, `${route}: no stylesheet loaded`).toBeGreaterThan(0);
	expect(styles.bodyBackground, `${route}: body has no background, page unstyled`).not.toBe(
		'rgba(0, 0, 0, 0)'
	);
	expect(styles.linkDecoration, `${route}: nav links keep the user-agent underline`).toBe('none');

	const { scrollWidth, innerWidth } = await page.evaluate(() => ({
		scrollWidth: document.documentElement.scrollWidth,
		innerWidth: window.innerWidth
	}));
	expect(scrollWidth, `${route}: horizontal page scroll`).toBeLessThanOrEqual(innerWidth);

	const misaligned = await page.evaluate(findMisalignedCells, TOLERANCE_PX);
	expect(misaligned, `${route}: header and body cells do not share columns`).toEqual([]);
}
