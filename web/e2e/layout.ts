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
	'/about',
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

export const MIN_TABLES: Record<(typeof AUTH_ROUTES)[number], number> = {
	'/': 0,
	'/about': 0,
	'/account': 1,
	'/configarr': 0,
	'/download-clients': 0,
	'/history': 1,
	'/instances': 1,
	'/instances/[id]': 0,
	'/instances/[id]/activity': 1,
	'/instances/[id]/memory': 0,
	'/instances/[id]/policy': 0,
	'/instances/[id]/queue': 1,
	'/live': 0,
	'/logs': 0,
	'/prowlarr': 1,
	'/schedules': 1,
	'/seerr': 1,
	'/settings': 2
};

// Routes whose table count follows the signed-in role and the data, not a fixed number: each
// list below renders one table when it is non-empty and an empty-state line otherwise.
const ROUTE_LISTS = {
	'/schedules': ['/api/v1/schedules'],
	'/settings': ['/api/v1/notifications/channels', '/api/v1/notifications/rules'],
	'/account': ['/api/v1/auth/api-keys']
} as const satisfies Partial<Record<(typeof AUTH_ROUTES)[number], readonly string[]>>;
type DataRoute = keyof typeof ROUTE_LISTS;
const DATA_LISTS: readonly string[] = Object.values(ROUTE_LISTS).flat();
const SESSION_PATH = '/api/v1/auth/session';

export type Role = 'admin' | 'viewer';

// Role matrix of ADR-0023 (docs/sso.md): a viewer reads every list above, but CreateAPIKey is
// admin-only, so a viewer's own key list stays empty and /account shows it no table.
const ROLE_LISTS: Record<Role, { readable: readonly string[]; neverFilled: readonly string[] }> = {
	admin: { readable: DATA_LISTS, neverFilled: [] },
	viewer: { readable: DATA_LISTS, neverFilled: ['/api/v1/auth/api-keys'] }
};

export function isDataRoute(route: string): route is DataRoute {
	return Object.hasOwn(ROUTE_LISTS, route);
}

// Tables a data route must show for this role, given the list lengths the page received. A
// readable list the page did not receive, or a list the role can never fill, fails the check.
export function expectedTables(
	route: DataRoute,
	role: Role,
	lengths: Readonly<Record<string, number>>
): number {
	const { readable, neverFilled } = ROLE_LISTS[role];
	let tables = 0;
	for (const list of ROUTE_LISTS[route]) {
		if (!readable.includes(list)) continue;
		const n = lengths[list];
		if (n === undefined) throw new Error(`${route}: GET ${list} gave the page no JSON list`);
		if (n > 0 && neverFilled.includes(list)) {
			throw new Error(`${route}: ${role} holds ${n} item(s) of ${list}; ADR-0023 forbids that`);
		}
		if (n > 0) tables += 1;
	}
	return tables;
}

export interface PageLists {
	role?: Role;
	lengths: Record<string, number>;
	reads: Promise<void>[];
}

// Records the session role and the list lengths the page fetches. Register before page.goto;
// works against the served site and against page.route mocks alike.
export function collectLists(page: Page): PageLists {
	const seen: PageLists = { lengths: {}, reads: [] };
	page.on('response', (res) => {
		if (res.request().method() !== 'GET' || !res.ok()) return;
		const path = new URL(res.url()).pathname;
		if (path === SESSION_PATH) {
			seen.reads.push(
				res.json().then((b: { user?: { role?: Role } }) => {
					seen.role = b.user?.role;
				})
			);
		} else if (DATA_LISTS.includes(path)) {
			seen.reads.push(
				res.json().then((b: unknown) => {
					if (Array.isArray(b)) seen.lengths[path] = b.length;
				})
			);
		}
	});
	return seen;
}

async function minTablesFor(route: string, lists?: PageLists): Promise<number | undefined> {
	if (!isDataRoute(route)) return undefined;
	expect(lists, `${route}: collectLists(page) was not registered before page.goto`).toBeDefined();
	await Promise.all(lists!.reads);
	expect(lists!.role, `${route}: no session role seen`).toMatch(/^(admin|viewer)$/);
	return expectedTables(route, lists!.role!, lists!.lengths);
}

// Allowed drift in CSS pixels between a header cell and the body cells below it.
const TOLERANCE_PX = 2;

export interface Box {
	x: number;
	width: number;
}

export interface ExtractedCell extends Box {
	scrollWidth: number;
}

export interface ExtractedRow {
	cells: ExtractedCell[];
	spans: boolean;
}

export interface ExtractedTable {
	kind: 'table' | 'grid';
	headers: Box[];
	rows: ExtractedRow[];
}

export interface Misaligned {
	table: number;
	row: number;
	column: number;
	header: Box;
	cell: Box;
	reason?: string;
}

// Extracts table and ARIA grid geometry for rendered-layout verification.
// Rows with differing cell count are findings unless a cell spans columns;
// headerless tables are findings; cell content overflowing its track is a finding.
export function extractTables(): ExtractedTable[] {
	const box = (el: Element): Box => {
		const r = el.getBoundingClientRect();
		return { x: Math.round(r.left), width: Math.round(r.width) };
	};
	const tables = Array.from(document.querySelectorAll('table, [role="grid"]')).slice(0, 50);
	return tables.map((t) => {
		const isGrid = t.getAttribute('role') === 'grid';
		const headers = isGrid
			? Array.from(t.querySelectorAll('[role="columnheader"]')).filter(
					(el) => el.closest('[role="grid"]') === t
				)
			: Array.from(t.querySelectorAll('th')).filter(
					(th) => th.closest('table') === t && th.closest('thead') !== null
				);
		const rawRows = isGrid
			? Array.from(t.querySelectorAll('[role="row"]')).filter(
					(r) => r.closest('[role="grid"]') === t && r.querySelector('[role="gridcell"]') !== null
				)
			: (t.querySelector('tbody')
					? Array.from(t.querySelectorAll('tbody > tr'))
					: Array.from(t.querySelectorAll('tr')).filter((tr) => tr.closest('thead') === null)
				).filter((r) => r.closest('table') === t);
		const rows = rawRows.slice(0, 500).map((row) => {
			const cells = isGrid
				? Array.from(row.querySelectorAll('[role="gridcell"]')).filter(
						(c) => c.closest('[role="row"]') === row
					)
				: Array.from(row.querySelectorAll(':scope > td, :scope > th')).filter(
						(c) => c.closest('tr') === row
					);
			const spans = cells.some((c) => {
				const cs = c.getAttribute('colspan') || c.getAttribute('aria-colspan');
				return cs !== null && parseInt(cs, 10) > 1;
			});
			return {
				spans,
				cells: cells.map((c) => ({ ...box(c), scrollWidth: c.scrollWidth }))
			};
		});
		return { kind: isGrid ? 'grid' : 'table', headers: headers.map(box), rows };
	});
}

function checkMissingHeaders(table: ExtractedTable, index: number): Misaligned | null {
	if (table.headers.length > 0) return null;
	return {
		table: index,
		row: -1,
		column: -1,
		header: { x: 0, width: 0 },
		cell: { x: 0, width: 0 },
		reason: table.kind === 'grid' ? 'grid without columnheader cells' : 'table without header cells'
	};
}

function checkCellCount(
	tableIdx: number,
	rowIdx: number,
	headerCount: number,
	row: ExtractedRow
): Misaligned | null {
	if (row.spans || row.cells.length === headerCount) return null;
	return {
		table: tableIdx,
		row: rowIdx,
		column: -1,
		header: { x: headerCount, width: headerCount },
		cell: { x: row.cells.length, width: row.cells.length },
		reason: `row cell count (${row.cells.length}) differs from header count (${headerCount})`
	};
}

function checkCellAlignment(
	tableIdx: number,
	rowIdx: number,
	colIdx: number,
	header: Box,
	cell: ExtractedCell,
	tolerance: number
): Misaligned | null {
	const headerBox = { x: header.x, width: header.width };
	const cellBox = { x: cell.x, width: cell.width };
	if (Math.abs(header.x - cell.x) > tolerance || Math.abs(header.width - cell.width) > tolerance) {
		return { table: tableIdx, row: rowIdx, column: colIdx, header: headerBox, cell: cellBox };
	}
	if (cell.scrollWidth > cell.width + tolerance) {
		return {
			table: tableIdx,
			row: rowIdx,
			column: colIdx,
			header: headerBox,
			cell: { x: cell.x, width: cell.scrollWidth },
			reason: `cell content (${cell.scrollWidth}px) overflows track (${cell.width}px)`
		};
	}
	return null;
}

export function checkTable(
	table: ExtractedTable,
	index: number,
	tolerance: number
): { measuredRows: number; findings: Misaligned[] } {
	const missing = checkMissingHeaders(table, index);
	if (missing) {
		return { measuredRows: 0, findings: [missing] };
	}
	let measuredRows = 0;
	const findings: Misaligned[] = [];
	for (let r = 0; r < table.rows.length; r++) {
		const row = table.rows[r];
		if (row.cells.length !== table.headers.length) {
			const countFinding = checkCellCount(index, r, table.headers.length, row);
			if (countFinding) findings.push(countFinding);
			continue;
		}
		measuredRows++;
		for (let c = 0; c < row.cells.length; c++) {
			const f = checkCellAlignment(index, r, c, table.headers[c], row.cells[c], tolerance);
			if (f) findings.push(f);
		}
	}
	return { measuredRows, findings };
}

function checkAllTables(
	tables: ExtractedTable[],
	tolerance: number
): { tableCount: number; measuredRows: number; misaligned: Misaligned[] } {
	let measuredRows = 0;
	const misaligned: Misaligned[] = [];
	for (let i = 0; i < tables.length; i++) {
		const res = checkTable(tables[i], i, tolerance);
		measuredRows += res.measuredRows;
		misaligned.push(...res.findings);
	}
	return { tableCount: tables.length, measuredRows, misaligned };
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

async function waitForSettled(page: Page): Promise<void> {
	await page.waitForLoadState('networkidle');
	await expect(
		page.locator(
			'[aria-busy="true"], [data-testid="skeleton"], [data-testid="live-skeleton"], [role="status"][aria-label*="loading" i], [role="status"][aria-label*="lädt" i], [role="status"][aria-label*="geladen" i]'
		)
	).toHaveCount(0);
	await expect(
		page.locator('p[aria-live="polite"]').filter({ hasText: /loading|lädt/i })
	).toHaveCount(0);
}

export async function assertRenderedLayout(
	page: Page,
	route: string,
	lists?: PageLists
): Promise<void> {
	expect(page.url(), `${route}: landed on /login (unauthenticated)`).not.toMatch(/\/login(?:\?|$)/);
	await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();

	await waitForSettled(page);

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

	const tables = await page.evaluate(extractTables);
	const { tableCount, measuredRows, misaligned } = checkAllTables(tables, TOLERANCE_PX);
	const exact = await minTablesFor(route, lists);
	const minTables = exact ?? MIN_TABLES[route as keyof typeof MIN_TABLES] ?? 0;
	if (exact === undefined) {
		expect(
			tableCount,
			`${route}: expected at least ${minTables} table(s), measured ${tableCount}`
		).toBeGreaterThanOrEqual(minTables);
	} else {
		expect(
			tableCount,
			`${route}: expected ${exact} table(s) for role ${lists?.role}, measured ${tableCount}`
		).toBe(exact);
	}
	if (minTables > 0) {
		expect(measuredRows, `${route}: declared a table but measured 0 body rows`).toBeGreaterThan(0);
	}
	expect(misaligned, `${route}: header and body cells do not share columns`).toEqual([]);
}
