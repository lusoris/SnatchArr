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
	desktop1920: { width: 1920, height: 1080 },
	phone: { width: 390, height: 844 },
	phone375: { width: 375, height: 812 },
	phone414: { width: 414, height: 896 }
} as const;

export const PHONE_FIT_ROUTES = [
	'/',
	'/account',
	'/automation',
	'/connections/download-clients',
	'/instances',
	'/system',
	'/system?tab=notifications'
] as const;

export const AUTH_ROUTES = [
	'/',
	'/account',
	'/activity',
	'/activity?source=live',
	'/activity?source=log',
	'/automation',
	'/automation?tab=caps',
	'/connections',
	'/connections/configarr',
	'/connections/download-clients',
	'/connections/prowlarr',
	'/connections/seerr',
	'/instances',
	'/instances/[id]',
	'/instances/[id]/activity',
	'/instances/[id]/memory',
	'/instances/[id]/policy',
	'/instances/[id]/queue',
	'/system',
	'/system?tab=modules',
	'/system?tab=notifications',
	'/system?tab=about'
] as const;

export const MIN_TABLES: Record<(typeof AUTH_ROUTES)[number], number> = {
	'/': 0,
	'/account': 1,
	'/activity': 0,
	'/activity?source=live': 0,
	'/activity?source=log': 0,
	'/automation': 1,
	'/automation?tab=caps': 0,
	'/connections': 0,
	'/connections/configarr': 0,
	'/connections/download-clients': 0,
	'/connections/prowlarr': 1,
	'/connections/seerr': 1,
	'/instances': 1,
	'/instances/[id]': 0,
	'/instances/[id]/activity': 1,
	'/instances/[id]/memory': 0,
	'/instances/[id]/policy': 0,
	'/instances/[id]/queue': 1,
	'/system': 0,
	'/system?tab=modules': 0,
	'/system?tab=notifications': 2,
	'/system?tab=about': 0
};

// Routes whose table count follows the signed-in role and the data, not a fixed number: each
// list below renders one table when it is non-empty and an empty-state line otherwise.
const ROUTE_LISTS = {
	'/': ['/api/v1/instances'],
	'/automation': ['/api/v1/schedules'],
	'/system?tab=notifications': ['/api/v1/notifications/channels', '/api/v1/notifications/rules'],
	'/account': ['/api/v1/auth/api-keys']
} as const satisfies Partial<Record<(typeof AUTH_ROUTES)[number], readonly string[]>>;
type DataRoute = keyof typeof ROUTE_LISTS;
const DATA_LISTS: readonly string[] = Object.values(ROUTE_LISTS).flat();
const SESSION_PATH = '/api/v1/auth/session';

export type Role = 'admin' | 'requester' | 'viewer';

// Role matrix of ADR-0023 (docs/sso.md) and ADR-0026: a viewer reads every list above, but CreateAPIKey is
// admin- or requester-only, so a viewer's own key list stays empty and /account shows it no table.
const ROLE_LISTS: Record<Role, { readable: readonly string[]; neverFilled: readonly string[] }> = {
	admin: { readable: DATA_LISTS, neverFilled: [] },
	requester: { readable: DATA_LISTS, neverFilled: [] },
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
	expect(lists!.role, `${route}: no session role seen`).toMatch(/^(admin|requester|viewer)$/);
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
	hasDataLabel?: boolean;
}

export interface ExtractedRow {
	cells: ExtractedCell[];
	spans: boolean;
	/** Card geometry; measured only on stacked tables (extractStackedGeometry). */
	card?: StackedCard;
}

export interface StackedCard {
	top: number;
	bottom: number;
	scrollHeight: number;
	clientHeight: number;
	/** The row renders no box (display: none), as a capped list row does. */
	hidden: boolean;
}

export interface StackedGeometry {
	table: number;
	headerHeight: number;
	cards: StackedCard[];
}

export interface ExtractedTable {
	kind: 'table' | 'grid';
	headers: Box[];
	rows: ExtractedRow[];
	stacked?: boolean;
	/** Rendered height of the header row of a stacked table, which app.css hides. */
	stackedHeaderHeight?: number;
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
	// Below 48rem app.css stacks .table-stack rows into labelled cards.
	const isPhoneWidth = window.matchMedia('(width < 48rem)').matches;
	const tables = Array.from(document.querySelectorAll('table, [role="grid"]')).slice(0, 50);
	return tables.map((t) => {
		const isGrid = t.getAttribute('role') === 'grid';
		const isStacked = !isGrid && t.classList.contains('table-stack') && isPhoneWidth;
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
				cells: cells.map((c) => ({
					...box(c),
					scrollWidth: c.scrollWidth,
					hasDataLabel: c.tagName.toLowerCase() === 'td' ? c.hasAttribute('data-label') : true
				}))
			};
		});
		return { kind: isGrid ? 'grid' : 'table', headers: headers.map(box), rows, stacked: isStacked };
	});
}

// Card geometry of every stacked table, indexed like extractTables: the hidden header's height
// and each row's extent, so a card that clips its fields or runs into the next one is caught.
export function extractStackedGeometry(): StackedGeometry[] {
	if (!window.matchMedia('(width < 48rem)').matches) return [];
	const tables = Array.from(document.querySelectorAll('table, [role="grid"]')).slice(0, 50);
	return tables.flatMap((t, table) => {
		if (t.getAttribute('role') === 'grid' || !t.classList.contains('table-stack')) return [];
		const head = t.querySelector(':scope > thead');
		const rows = Array.from(t.querySelectorAll('tbody > tr')).filter(
			(r) => r.closest('table') === t
		);
		const cards = rows.slice(0, 500).map((row) => {
			const g = row.getBoundingClientRect();
			return {
				top: Math.round(g.top),
				bottom: Math.round(g.bottom),
				scrollHeight: row.scrollHeight,
				clientHeight: row.clientHeight,
				hidden: row.getClientRects().length === 0
			};
		});
		return [{ table, headerHeight: Math.round(head?.getBoundingClientRect().height ?? 0), cards }];
	});
}

export function withStackedGeometry(
	tables: ExtractedTable[],
	geometry: StackedGeometry[]
): ExtractedTable[] {
	return tables.map((t, i) => {
		const g = geometry.find((x) => x.table === i);
		if (!g || !t.stacked) return t;
		return {
			...t,
			stackedHeaderHeight: g.headerHeight,
			rows: t.rows.map((row, r) => ({ ...row, card: g.cards[r] }))
		};
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

function stackedFinding(index: number, row: number, column: number, reason: string): Misaligned {
	return {
		table: index,
		row,
		column,
		header: { x: 0, width: 0 },
		cell: { x: 0, width: 0 },
		reason
	};
}

// A stacked row is a card: its header row stays hidden, every cell carries its label, and each
// card holds all of its cells without running into the next card.
function checkStackedRow(
	table: ExtractedTable,
	index: number,
	r: number,
	tolerance: number
): Misaligned[] {
	const row = table.rows[r];
	const findings = row.cells.flatMap((cell, c) =>
		cell.hasDataLabel === false
			? [
					{
						...stackedFinding(index, r, c, 'stacked table cell missing data-label attribute'),
						header: table.headers[c] ?? { x: 0, width: 0 },
						cell: { x: cell.x, width: cell.width }
					}
				]
			: []
	);
	const card = row.card;
	if (!card || card.hidden) return findings;
	if (card.scrollHeight > card.clientHeight + tolerance) {
		findings.push(
			stackedFinding(
				index,
				r,
				-1,
				`stacked row content (${card.scrollHeight}px) overflows its card (${card.clientHeight}px)`
			)
		);
	}
	// The next card that renders a box; a capped row hidden between them has none.
	const next = table.rows.slice(r + 1).find((n) => n.card && !n.card.hidden)?.card;
	if (next && card.bottom > next.top + tolerance) {
		findings.push(
			stackedFinding(
				index,
				r,
				-1,
				`stacked row ends at ${card.bottom}px, below the next row's top at ${next.top}px`
			)
		);
	}
	return findings;
}

function checkStackedTable(
	table: ExtractedTable,
	index: number,
	tolerance: number
): { measuredRows: number; findings: Misaligned[] } {
	const findings: Misaligned[] = [];
	if ((table.stackedHeaderHeight ?? 0) > 1) {
		findings.push(
			stackedFinding(
				index,
				-1,
				-1,
				`stacked table header is visible (${table.stackedHeaderHeight}px tall)`
			)
		);
	}
	for (let r = 0; r < table.rows.length; r++) {
		findings.push(...checkStackedRow(table, index, r, tolerance));
	}
	return { measuredRows: table.rows.length, findings };
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
	if (table.stacked) {
		return checkStackedTable(table, index, tolerance);
	}
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

export interface LabelValueOffender {
	text: string;
	gap: number;
}

function checkLabelValueGaps(): LabelValueOffender[] {
	const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-label-value]'));
	const offenders: LabelValueOffender[] = [];
	for (const row of rows) {
		const first = row.firstElementChild;
		const last = row.lastElementChild;
		if (!first || !last || first === last) continue;
		const a = first.getBoundingClientRect();
		const b = last.getBoundingClientRect();
		if (a.top < b.bottom && b.top < a.bottom) {
			const gap = b.left - a.right;
			if (gap < 8) {
				offenders.push({ text: first.textContent?.trim() ?? '', gap });
			}
		}
	}
	return offenders;
}

export interface UrlBreakOffender {
	url: string;
	prevChar: string;
	nextChar: string;
	segment: string;
	segmentWidth: number;
	elementWidth: number;
}

type CPos = { c: string; n: Text; o: number };
type Cand = { u: string; el: HTMLElement; ch: CPos[] };
export function checkUrlBreaks(): UrlBreakOffender[] {
	const d = /[.:?&#/]/,
		cands: Cand[] = [],
		offenders: UrlBreakOffender[] = [],
		tw = document.createTreeWalker(document.body, 4),
		range = document.createRange(),
		m = document.body.appendChild(document.createElement('span'));
	m.style.cssText = 'white-space:nowrap;visibility:hidden;position:absolute';
	const pos = (n: Text, s = n.data, o = 0) => [...s].map((c, i) => ({ c, n, o: o + i }));
	let tn: Text | null,
		scanned = 0;
	while ((tn = tw.nextNode() as Text | null) && cands.length < 200 && scanned < 20000) {
		const p = tn.parentElement;
		if (!p || !p.offsetWidth || !p.offsetHeight) continue;
		const du = p.closest<HTMLElement>('[data-url]');
		if (du && !du.dataset.scanned) {
			du.dataset.scanned = '1';
			const ch = [...du.childNodes].flatMap((n) => (n.nodeType === 3 ? pos(n as Text) : []));
			scanned += ch.length;
			cands.push({ u: du.textContent ?? '', el: du, ch });
		} else if (!du && tn.data.includes('http')) {
			if (p.closest('input, textarea, select, script, style, details:not([open])')) continue;
			scanned += tn.data.length;
			for (const mt of tn.data.matchAll(/https?:\/\/[^\s]+/g))
				cands.push({ u: mt[0], el: p, ch: pos(tn, mt[0], mt.index) });
		}
	}
	for (const { u, el, ch } of cands) {
		if (offenders.some((o) => o.url === u)) continue;
		let b: HTMLElement = el;
		while (b.parentElement && getComputedStyle(b).display === 'inline') b = b.parentElement;
		const elW = b.getBoundingClientRect().width;
		m.style.font = getComputedStyle(el).font;
		for (let i = 0, prevTop = 0; i < ch.length; i++) {
			range.setStart(ch[i].n, ch[i].o);
			range.setEnd(ch[i].n, ch[i].o + 1);
			const r = range.getBoundingClientRect();
			const isBrk = i > 0 && r.top > prevTop + 4 && !d.test(ch[i - 1].c) && !d.test(ch[i].c);
			prevTop = r.top;
			if (!isBrk) continue;
			let [s, e] = [i - 1, i];
			while (s > 0 && !d.test(u[s]) && u.slice(s - 3, s) !== '://') s--;
			while (e < u.length && !d.test(u[e])) e++;
			m.textContent = u.slice(s, e);
			if (m.getBoundingClientRect().width <= elW) {
				offenders.push({
					url: u,
					prevChar: ch[i - 1].c,
					nextChar: ch[i].c,
					segment: u.slice(s, e),
					segmentWidth: Math.round(m.getBoundingClientRect().width),
					elementWidth: Math.round(elW)
				});
				break;
			}
		}
	}
	m.remove();
	return offenders;
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

	const offenders = await page.evaluate(checkLabelValueGaps);
	expect(offenders, `${route}: label and value touch`).toEqual([]);

	const urlOffenders = await page.evaluate(checkUrlBreaks);
	expect(urlOffenders, `${route}: URL wrapped mid-token`).toEqual([]);

	// A list arrives after the first paint, so wait for the tables it must render before measuring.
	const exact = await minTablesFor(route, lists);
	if (exact) {
		await expect
			.poll(() => page.locator('table, [role="grid"]').count(), {
				message: `${route}: expected ${exact} table(s) to render`
			})
			.toBeGreaterThanOrEqual(exact);
	}
	const tables = withStackedGeometry(
		await page.evaluate(extractTables),
		await page.evaluate(extractStackedGeometry)
	);
	const { tableCount, measuredRows, misaligned } = checkAllTables(tables, TOLERANCE_PX);
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

export interface ContentSpan {
	mainWidth: number;
	widest: number;
	ratio: number;
}

export function measureContentSpan(): ContentSpan {
	const main = document.querySelector('main');
	if (!main) return { mainWidth: 0, widest: 0, ratio: 0 };
	const mainStyle = window.getComputedStyle(main);
	const padLeft = parseFloat(mainStyle.paddingLeft) || 0;
	const padRight = parseFloat(mainStyle.paddingRight) || 0;
	const mainWidth = Math.round(main.clientWidth - padLeft - padRight);

	const selector =
		'section, .card, table, form, [role="table"], [role="grid"], [role="log"], ul, dl';
	const elements = Array.from(main.querySelectorAll(selector));
	let widest = 0;
	for (const el of elements) {
		const style = window.getComputedStyle(el);
		if (style.display === 'none' || style.visibility === 'hidden') continue;
		const rect = el.getBoundingClientRect();
		const width = Math.round(rect.width);
		if (width > 0 && width > widest) {
			widest = width;
		}
	}
	const ratio = mainWidth > 0 ? widest / mainWidth : 0;
	return { mainWidth, widest, ratio };
}

export async function assertDesktopWidth(page: Page, route: string): Promise<void> {
	const { mainWidth, widest, ratio } = await page.evaluate(measureContentSpan);
	if (widest === 0) {
		throw new Error(`${route}: no content block measured`);
	}
	if (ratio < 0.75) {
		throw new Error(
			`${route}: widest content block ${widest}px is narrower than 75% of main (${mainWidth}px)`
		);
	}
}

export interface PhoneFitOffender {
	tag: string;
	cls: string;
	width: number;
	scrollWidth: number;
}

export interface PhoneFitResult {
	mainWidth: number;
	offenders: PhoneFitOffender[];
}

// Flags blocks wider than main's content box and inner horizontal scroll
// containers: on PHONE_FIT_ROUTES every column must fit without scrolling.
export function measurePhoneFit(): PhoneFitResult {
	const main = document.querySelector('main');
	if (!main) return { mainWidth: 0, offenders: [] };
	const mainStyle = window.getComputedStyle(main);
	const mainWidth = Math.round(
		main.clientWidth - parseFloat(mainStyle.paddingLeft) - parseFloat(mainStyle.paddingRight)
	);
	const blocks = 'section, .card, table, form, [role="table"], [role="grid"], [role="log"], ul, dl';
	const offenders: PhoneFitOffender[] = [];
	for (const el of Array.from(main.querySelectorAll('*'))) {
		const style = window.getComputedStyle(el);
		if (style.display === 'none' || style.visibility === 'hidden') continue;
		const width = Math.round(el.getBoundingClientRect().width);
		const tooWide = el.matches(blocks) && width > mainWidth + 1;
		const scrolls =
			(style.overflowX === 'auto' || style.overflowX === 'scroll') &&
			el.scrollWidth > el.clientWidth + 1;
		if (!tooWide && !scrolls) continue;
		const cls = (el.getAttribute('class') ?? '').slice(0, 60);
		offenders.push({ tag: el.tagName.toLowerCase(), cls, width, scrollWidth: el.scrollWidth });
	}
	return { mainWidth, offenders };
}

export async function assertPhoneFit(page: Page, route: string): Promise<void> {
	const { offenders } = await page.evaluate(measurePhoneFit);
	if (offenders.length > 0) {
		throw new Error(
			`${route}: content wider than main at phone width (${offenders.length} block(s)): ${JSON.stringify(offenders)}`
		);
	}
}

// Scroll budget by route class (ADR-0029 section 4). Main pages (overviews, lists, hubs, tabs)
// fit 1.25x a 1280x800 screen. Edit forms (the settings of one entity or one user) and the
// public pages sit outside the budget by class, never by a per-route ceiling. Every page route
// under src/routes carries a class; layout.test.ts fails on an unclassified or stale entry.
export type RouteClass = 'main' | 'edit-form' | 'public';

export const ROUTE_CLASS: Record<string, RouteClass> = {
	'/': 'main',
	'/activity': 'main',
	'/activity?source=live': 'main',
	'/activity?source=log': 'main',
	'/adult/image-sets': 'main',
	'/adult/performers': 'main',
	'/adult/sources': 'main',
	'/adult/studios': 'main',
	'/automation': 'main',
	'/automation?tab=caps': 'main',
	'/connections': 'main',
	'/connections/configarr': 'main',
	'/connections/download-clients': 'main',
	'/connections/prowlarr': 'main',
	'/connections/seerr': 'main',
	'/instances': 'main',
	'/instances/[id]': 'main',
	'/instances/[id]/activity': 'main',
	'/instances/[id]/memory': 'main',
	'/system': 'main',
	'/system?tab=modules': 'main',
	'/system?tab=notifications': 'main',
	'/system?tab=about': 'main',
	'/account': 'edit-form',
	'/instances/[id]/policy': 'edit-form',
	'/instances/[id]/queue': 'edit-form',
	'/settings/requests': 'edit-form',
	'/login': 'public',
	'/setup': 'public'
};

function routesOfClass(cls: RouteClass): string[] {
	return Object.entries(ROUTE_CLASS)
		.filter(([, c]) => c === cls)
		.map(([route]) => route);
}

export const SCROLL_BUDGET_ROUTES = routesOfClass('main');
export const EDIT_FORM_ROUTES = routesOfClass('edit-form');

// Activity views scroll inside their feed, so the document itself never scrolls.
const FEED_ROUTES = new Set(['/activity', '/activity?source=live', '/activity?source=log']);

export async function assertScrollBudget(
	page: Page,
	route: string,
	maxBudget?: number
): Promise<void> {
	const { scrollHeight, innerHeight } = await page.evaluate(() => ({
		scrollHeight: document.documentElement.scrollHeight,
		innerHeight: window.innerHeight
	}));
	const limit = maxBudget ?? (FEED_ROUTES.has(route) ? innerHeight + 2 : 1.25 * innerHeight);
	expect(
		scrollHeight,
		`${route}: scroll height (${scrollHeight}px) exceeds budget (${limit}px)`
	).toBeLessThanOrEqual(limit);
}

export async function assertTableFits(page: Page, tableSelector = 'table'): Promise<void> {
	const fit = await page.evaluate((sel) => {
		const table = document.querySelector<HTMLElement>(sel);
		if (!table) return null;
		const container = table.closest<HTMLElement>('.overflow-x-auto') ?? table.parentElement;
		if (!container) return null;
		return {
			scrollWidth: table.scrollWidth,
			clientWidth: container.clientWidth
		};
	}, tableSelector);

	expect(fit, `table matching "${tableSelector}" not found in page`).not.toBeNull();
	expect(
		fit!.scrollWidth,
		`table (${fit!.scrollWidth}px) exceeds container (${fit!.clientWidth}px)`
	).toBeLessThanOrEqual(fit!.clientWidth);
}

export interface PendingRoute {
	pr: 'C' | 'D' | 'E' | 'F' | 'G';
	/** ISO date (YYYY-MM-DD); past it the entry fails like a missing one. */
	expires: string;
	reason: string;
}

export const WIDTH_PENDING: Record<string, PendingRoute> = {};

export interface TouchTargetOffender {
	selector: string;
	height: number;
}

export function measureTouchTargets(): TouchTargetOffender[] {
	const selector =
		'.btn, .input, .select, .nav-link, .subtab-link, [role="tab"], nav a[href], nav button';
	const elements = Array.from(document.querySelectorAll(selector));
	const offenders: TouchTargetOffender[] = [];
	for (const el of elements) {
		if (
			typeof el.checkVisibility === 'function' &&
			!el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
		) {
			continue;
		}
		const rect = el.getBoundingClientRect();
		if (rect.width <= 1 || rect.height <= 1) continue;
		const height = Math.round(rect.height * 2) / 2;
		if (height < 44) {
			const tag = el.tagName.toLowerCase();
			const cls = (el.getAttribute('class') ?? '').trim().replace(/\s+/g, ' ');
			const name =
				el.getAttribute('aria-label') ??
				el.getAttribute('title') ??
				(el.textContent ? el.textContent.trim().replace(/\s+/g, ' ').slice(0, 40) : '');
			const desc = `${tag}${cls ? `.${cls.split(' ').join('.')}` : ''}${name ? ` "${name}"` : ''}`;
			offenders.push({ selector: desc, height });
		}
	}
	return offenders;
}

export async function assertTouchTargetHeights(page: Page, route: string): Promise<void> {
	const offenders = await page.evaluate(measureTouchTargets);
	if (offenders.length > 0) {
		const summary = offenders.map((o) => `  - ${o.selector}: ${o.height}px < 44px`).join('\n');
		throw new Error(
			`${route}: touch target height < 44px on phone (${offenders.length} element(s)):\n${summary}`
		);
	}
}

export interface GridRowOffender {
	grid: string;
	row: number;
	spread: number;
}

// Runs inside page.evaluate, so every helper lives in the function body.
// Children whose boxes overlap vertically share a grid row; their first
// input or select must start at the same top edge.
export function measureGridRowAlignment(): GridRowOffender[] {
	const visible = (el: Element): boolean => el.checkVisibility({ checkVisibilityCSS: true });
	const control = (child: Element): HTMLElement | null =>
		child.matches('input, select') ? (child as HTMLElement) : child.querySelector('input, select');
	const offenders: GridRowOffender[] = [];
	for (const grid of Array.from(document.querySelectorAll<HTMLElement>('main .grid'))) {
		if (!visible(grid)) continue;
		const cells = Array.from(grid.children)
			.filter((child) => visible(child) && control(child) !== null)
			.map((child) => ({
				box: child.getBoundingClientRect(),
				top: control(child)!.getBoundingClientRect().top
			}))
			.sort((x, y) => x.box.top - y.box.top);
		const rows: (typeof cells)[] = [];
		for (const cell of cells) {
			const row = rows.at(-1);
			if (row && cell.box.top < Math.max(...row.map((c) => c.box.bottom)) - 1) row.push(cell);
			else rows.push([cell]);
		}
		for (const row of rows) {
			const tops = row.map((c) => c.top);
			const spread = Math.round((Math.max(...tops) - Math.min(...tops)) * 100) / 100;
			if (spread <= 1) continue;
			const legend = grid.closest('fieldset')?.querySelector('legend')?.textContent?.trim();
			offenders.push({ grid: legend ?? 'grid', row: Math.round(row[0].box.top), spread });
		}
	}
	return offenders;
}

export async function assertGridRowAlignment(page: Page, route: string): Promise<void> {
	const offenders = await page.evaluate(measureGridRowAlignment);
	if (offenders.length > 0) {
		const list = offenders
			.map((o) => `${o.grid} (row ${o.row}px, spread ${o.spread}px)`)
			.join('; ');
		throw new Error(
			`${route}: inputs misaligned in grid row (${offenders.length} row(s)): ${list}`
		);
	}
}

/**
 * A capped dashboard card keeps every row in a bounded body that scrolls inside the card: the
 * body is a labelled region in the tab order, it is shorter than its content, and after the
 * keyboard scrolls it to the end the card's last row lies inside the body's box.
 */
export async function assertCardScrollsToLast(
	page: Page,
	region: string,
	lastRow: string
): Promise<void> {
	const body = page.getByRole('region', { name: `${region}, scrollable list`, exact: true });
	await expect(body, `${region}: no scrolling region`).toBeVisible();
	await expect(body, `${region}: region is not in the tab order`).toHaveAttribute('tabindex', '0');
	const sizes = await body.evaluate((el) => ({
		scrollHeight: el.scrollHeight,
		clientHeight: el.clientHeight
	}));
	expect(
		sizes.scrollHeight,
		`${region}: body does not scroll (${sizes.scrollHeight}px content in ${sizes.clientHeight}px)`
	).toBeGreaterThan(sizes.clientHeight);
	await body.focus();
	await page.keyboard.press('End');
	const row = body.getByText(lastRow, { exact: true }).first();
	await expect(row, `${region}: last row "${lastRow}" missing from the card`).toBeAttached();
	await expect
		.poll(
			async () => {
				const [b, r] = await Promise.all([body.boundingBox(), row.boundingBox()]);
				if (!b || !r) return false;
				return r.y >= b.y - 1 && r.y + r.height <= b.y + b.height + 1;
			},
			{ message: `${region}: last row "${lastRow}" not reachable inside the card` }
		)
		.toBe(true);
}
