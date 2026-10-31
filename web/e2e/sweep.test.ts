// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import {
	expect,
	test,
	type BrowserContext,
	type Locator,
	type Page,
	type Response
} from '@playwright/test';
import { settleToasts } from './settle.js';

interface ControlDescriptor {
	key: string;
	role: string;
	name: string;
	index: number;
	tag: string;
	kind: string;
	isDestructive: boolean;
	isSkip: boolean;
	skipReason?: string;
	domIndex: number;
	srOnly: boolean;
}

interface DiscoveredTab {
	tab: string;
	url?: string;
	selector?: string;
}

interface SweepRow {
	route: string;
	tab: string;
	control: string;
	kind: string;
	verdict: 'pass' | 'warn' | 'fail' | 'skip';
	api_fail: string;
	console: string;
	detail: string;
}

interface ActionErrors {
	pageErrors: string[];
	consoleErrors: string[];
	apiFailures: string[];
	apiWarnings: string[];
	externalRequests: string[];
	destructiveRequests: string[];
	pending: Promise<void>[];
}

const STATIC_ROUTES = [
	'/',
	'/account',
	'/activity',
	'/automation',
	'/connections',
	'/connections/configarr',
	'/connections/download-clients',
	'/connections/prowlarr',
	'/connections/seerr',
	'/instances',
	'/settings/requests',
	'/system',
	'/adult/image-sets',
	'/adult/performers',
	'/adult/sources',
	'/adult/studios'
];

const CONTROL_QUERY =
	'button, [role="button"], [role="tab"], [role="switch"], [role="menuitem"], summary, select, input[type="checkbox"], a[href]';
const LOGOUT_RE = /log ?out|sign ?out|abmelden/i;

// Best-effort steps (settling, closing, cancelling) may fail without failing the row; each
// failure is counted and printed with the summary instead of being dropped.
const softFailures: string[] = [];

async function attempt(step: Promise<unknown>): Promise<void> {
	try {
		await step;
	} catch (err: unknown) {
		softFailures.push(lastLogLine(err instanceof Error ? err.message : String(err)));
	}
}
const DESTRUCTIVE_RE = /delete|remove|revoke|reset|löschen|entfernen|zurücksetzen/i;
const CLIENT_ERROR_LOG_RE =
	/^Failed to load resource: the server responded with a status of 4\d\d\b/;
const UUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;

function isLocalUrl(rawUrl: string): boolean {
	if (rawUrl.startsWith('data:') || rawUrl.startsWith('blob:') || rawUrl.startsWith('about:')) {
		return true;
	}
	try {
		const u = new URL(rawUrl);
		return u.hostname === '127.0.0.1' || u.hostname === 'localhost' || u.hostname === '::1';
	} catch {
		return false;
	}
}

function sanitizeApiPath(rawUrl: string, method: string, status?: number): string {
	try {
		const u = new URL(rawUrl);
		const sanitized = u.pathname.replace(UUID_RE, ':id');
		return status ? `${method} ${sanitized} (${status})` : `${method} ${sanitized}`;
	} catch {
		return `${method} ${rawUrl}`;
	}
}

function sanitizeTsv(val: string): string {
	return val.replace(/[\t\r\n]+/g, ' ').trim();
}

function formatRow(row: SweepRow): string {
	return [
		row.route,
		row.tab,
		row.control,
		row.kind,
		row.verdict,
		row.api_fail,
		row.console,
		row.detail
	]
		.map(sanitizeTsv)
		.join('\t');
}

function resetErrors(errors: ActionErrors): void {
	errors.pageErrors.length = 0;
	errors.consoleErrors.length = 0;
	errors.apiFailures.length = 0;
	errors.apiWarnings.length = 0;
	errors.externalRequests.length = 0;
	errors.destructiveRequests.length = 0;
	errors.pending.length = 0;
}

// Problem bodies are read off the event loop; collect them before a verdict.
async function drainPending(errors: ActionErrors): Promise<void> {
	await Promise.allSettled(errors.pending);
}

async function problemDetail(res: Response): Promise<string> {
	try {
		const body = (await res.json()) as { title?: string; detail?: string };
		return (body.detail ?? body.title ?? '').slice(0, 160);
	} catch {
		return '';
	}
}

async function fetchInstanceRoutes(page: Page): Promise<string[]> {
	try {
		const res = await page.request.get('/api/v1/instances');
		if (!res.ok()) return [];
		const list = (await res.json()) as Array<{ id?: string }>;
		const routes: string[] = [];
		for (const inst of list) {
			if (!inst.id) continue;
			routes.push(
				`/instances/${inst.id}`,
				`/instances/${inst.id}/activity`,
				`/instances/${inst.id}/memory`,
				`/instances/${inst.id}/policy`,
				`/instances/${inst.id}/queue`
			);
		}
		return routes;
	} catch {
		return [];
	}
}

function isDestructiveRequest(method: string, rawUrl: string): boolean {
	if (!rawUrl.includes('/api/')) return false;
	return method === 'DELETE' || (method === 'POST' && rawUrl.includes('/api/v1/state/reset'));
}

async function setupNetworkGuard(context: BrowserContext, errors: ActionErrors): Promise<void> {
	await context.route('**/*', async (route) => {
		const req = route.request();
		const raw = req.url();
		if (!isLocalUrl(raw)) {
			errors.externalRequests.push(raw);
			await attempt(route.abort('blockedbyclient'));
			return;
		}
		// The sweep only ever cancels a destructive step, so a request that destroys state means
		// the confirm step let it through; refuse it so later pages still find their fixtures.
		if (isDestructiveRequest(req.method(), raw)) {
			errors.destructiveRequests.push(sanitizeApiPath(raw, req.method()));
			await attempt(route.abort('blockedbyclient'));
			return;
		}
		await attempt(route.continue());
	});
}

function setupListeners(page: Page, errors: ActionErrors): void {
	page.on('pageerror', (err) => {
		errors.pageErrors.push(err.message);
	});
	page.on('console', (msg) => {
		if (msg.type() !== 'error') return;
		// Chromium logs every non-2xx fetch; a 4xx is already a warn from the response listener.
		if (CLIENT_ERROR_LOG_RE.test(msg.text())) return;
		errors.consoleErrors.push(msg.text());
	});
	page.on('response', (res) => {
		if (!res.url().includes('/api/')) return;
		const status = res.status();
		if (status < 400) return;
		const method = res.request().method();
		const entry = sanitizeApiPath(res.url(), method, status);
		const list = status >= 500 ? errors.apiFailures : errors.apiWarnings;
		errors.pending.push(
			problemDetail(res).then((detail) => {
				list.push(detail ? `${entry}: ${detail}` : entry);
			})
		);
	});
	page.on('requestfailed', (req) => {
		if (!req.url().includes('/api/')) return;
		const failure = req.failure();
		const detail = failure ? failure.errorText : 'failed';
		// Leaving a page cancels its open requests (the event stream above all); not a failure.
		if (detail === 'net::ERR_ABORTED') return;
		// Refused by the guard above, already recorded there.
		if (detail === 'net::ERR_BLOCKED_BY_CLIENT') return;
		const entry = `${sanitizeApiPath(req.url(), req.method())} (${detail})`;
		errors.apiFailures.push(entry);
	});
}

async function navigateAndSettle(page: Page, url: string, selector?: string): Promise<void> {
	await page.goto(url, { timeout: 20_000, waitUntil: 'domcontentloaded' });
	await attempt(page.waitForLoadState('networkidle', { timeout: 10_000 }));
	if (selector) {
		const target = page.locator(selector).first();
		if (await target.isVisible()) {
			await attempt(target.click({ timeout: 5000 }));
		}
	}
	await attempt(settleToasts(page));
}

interface RawControl {
	domIndex: number;
	shown: boolean;
	disabled: boolean;
	tag: string;
	roleAttr: string;
	inputType: string;
	href: string;
	label: string;
	srOnly: boolean;
}

const IMPLICIT_ROLE: Record<string, string> = {
	button: 'button',
	a: 'link',
	select: 'combobox',
	summary: 'summary'
};

function deriveRole(raw: RawControl): string {
	if (raw.roleAttr) return raw.roleAttr;
	if (raw.tag === 'input' && raw.inputType === 'checkbox') return 'checkbox';
	return IMPLICIT_ROLE[raw.tag] ?? '';
}

function isSweepable(raw: RawControl, origin: string): boolean {
	if (!raw.shown || raw.disabled) return false;
	if (raw.tag !== 'a' || !raw.href) return true;
	return URL.canParse(raw.href) && new URL(raw.href).origin === origin;
}

function toDescriptor(raw: RawControl, index: number, role: string): ControlDescriptor {
	const name = raw.label;
	const isSkip = LOGOUT_RE.test(name);
	return {
		key: `${role}:${name}:${index}`,
		role,
		name,
		index,
		tag: raw.tag,
		kind: raw.tag === 'select' || raw.tag === 'summary' ? raw.tag : role || raw.tag,
		isDestructive: !isSkip && DESTRUCTIVE_RE.test(name),
		isSkip,
		skipReason: isSkip ? 'logout control (keeps session)' : undefined,
		domIndex: raw.domIndex,
		srOnly: raw.srOnly
	};
}

// Runs in the page: raw facts only, every decision is made in Node below.
async function readRawControls(page: Page): Promise<RawControl[]> {
	return page.locator(CONTROL_QUERY).evaluateAll((elements) =>
		elements.map((node, domIndex) => {
			const el = node as HTMLElement;
			const rect = el.getBoundingClientRect();
			const labelledBy = (el.getAttribute('aria-labelledby') ?? '')
				.split(/\s+/)
				.map((id) => (id ? (document.getElementById(id)?.innerText ?? '') : ''))
				.filter(Boolean)
				.join(' ');
			const text = el.innerText || el.textContent || el.title || el.getAttribute('placeholder');
			return {
				domIndex,
				shown:
					el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) &&
					(rect.width > 0 || rect.height > 0),
				disabled: (el as HTMLButtonElement).disabled || el.ariaDisabled === 'true',
				tag: el.tagName.toLowerCase(),
				roleAttr: el.getAttribute('role') ?? '',
				inputType: (el as HTMLInputElement).type ?? '',
				href: (el as HTMLAnchorElement).href ?? '',
				label: (el.ariaLabel || labelledBy || text || '').trim().replace(/\s+/g, ' '),
				srOnly: rect.width <= 1 || rect.height <= 1
			};
		})
	);
}

async function enumerateControls(page: Page): Promise<ControlDescriptor[]> {
	const origin = new URL(page.url()).origin;
	const counts = new Map<string, number>();
	const found: ControlDescriptor[] = [];
	for (const raw of await readRawControls(page)) {
		if (found.length >= 120) break;
		if (!isSweepable(raw, origin)) continue;
		const role = deriveRole(raw);
		const countKey = `${role}:::${raw.label}`;
		const index = counts.get(countKey) ?? 0;
		counts.set(countKey, index + 1);
		found.push(toDescriptor(raw, index, role));
	}
	return found;
}

// Earlier controls can change persisted state (the locale picker, a module toggle) and live
// rows carry request ids, so the fresh page is enumerated again: the key finds the control,
// else the same DOM position with the same role (only its text changed). Role + name
// locators match substrings and land on hidden duplicates.
async function resolveControl(
	page: Page,
	ctrl: ControlDescriptor
): Promise<{ target: ControlDescriptor; note: string } | undefined> {
	const current = await enumerateControls(page);
	const byKey = current.find((c) => c.key === ctrl.key);
	if (byKey) return { target: byKey, note: '' };
	const byPos = current.find((c) => c.domIndex === ctrl.domIndex && c.role === ctrl.role);
	if (!byPos) return undefined;
	return { target: byPos, note: `relocated by position, now ${byPos.key.slice(0, 80)}` };
}

function skipRow(route: string, tab: string, ctrl: ControlDescriptor, detail: string): SweepRow {
	return {
		route,
		tab,
		control: ctrl.key,
		kind: ctrl.kind,
		verdict: 'skip',
		api_fail: '',
		console: '',
		detail
	};
}

async function interactSelect(loc: Locator): Promise<void> {
	const options = await loc.locator('option').all();
	if (options.length > 1) {
		const val = await options[1].getAttribute('value');
		if (val !== null) {
			await loc.selectOption(val, { timeout: 5000 });
			return;
		}
		await loc.selectOption({ index: 1 }, { timeout: 5000 });
		return;
	}
	if (options.length === 1) {
		await loc.selectOption({ index: 0 }, { timeout: 5000 });
	}
}

async function handleDestructiveCancel(page: Page): Promise<void> {
	const dialog = page.locator('dialog[open], [role="dialog"]:not([aria-hidden="true"])').first();
	if (await dialog.isVisible()) {
		const cancelInDialog = dialog
			.locator(
				'button:has-text("Cancel"), button:has-text("Abbrechen"), button:has-text("Back"), button:has-text("Zurück"), button:has-text("Nein"), button:has-text("No"), button[aria-label*="cancel" i], button[aria-label*="abbrechen" i]'
			)
			.first();
		if (await cancelInDialog.isVisible()) {
			await attempt(cancelInDialog.click({ timeout: 3000 }));
			return;
		}
	}
	const inlineCancel = page
		.locator(
			'[role="group"] button:has-text("Cancel"), [role="group"] button:has-text("Abbrechen"), [role="group"] button:has-text("Back"), [role="group"] button:has-text("Zurück")'
		)
		.first();
	if (await inlineCancel.isVisible()) {
		await attempt(inlineCancel.click({ timeout: 3000 }));
	}
}

async function closeDialogIfOpen(page: Page): Promise<{ opened: boolean; closed: boolean }> {
	const dialog = page.locator('dialog[open], [role="dialog"]:not([aria-hidden="true"])').first();
	if (!(await dialog.isVisible())) {
		return { opened: false, closed: true };
	}
	await page.keyboard.press('Escape');
	await page.waitForTimeout(200);
	if (!(await dialog.isVisible())) {
		return { opened: true, closed: true };
	}
	const closeBtn = dialog
		.locator(
			'button[aria-label*="close" i], button[aria-label*="schließen" i], button:has-text("Close"), button:has-text("Schließen"), button:has-text("Cancel"), button:has-text("Abbrechen")'
		)
		.first();
	if (await closeBtn.isVisible()) {
		await attempt(closeBtn.click({ timeout: 2000 }));
		await page.waitForTimeout(200);
	}
	const stillOpen = await dialog.isVisible();
	return { opened: true, closed: !stillOpen };
}

async function interactControl(
	page: Page,
	loc: Locator,
	ctrl: ControlDescriptor
): Promise<{ timeout: boolean; error?: string }> {
	try {
		if (ctrl.srOnly) {
			// Visually hidden until focused (skip link): keyboard is the only way to use it.
			await loc.focus({ timeout: 5000 });
			await page.keyboard.press('Enter');
		} else if (ctrl.kind === 'select' || ctrl.tag === 'select') {
			await interactSelect(loc);
		} else if (ctrl.role === 'checkbox' || (ctrl.tag === 'input' && ctrl.kind === 'checkbox')) {
			await loc.click({ timeout: 5000 });
		} else if (ctrl.role === 'link' || ctrl.tag === 'a') {
			await loc.click({ timeout: 5000 });
			await attempt(page.waitForLoadState('domcontentloaded', { timeout: 5000 }));
		} else {
			await loc.click({ timeout: 5000 });
		}
		return { timeout: false };
	} catch (err: unknown) {
		const msg = err instanceof Error ? err.message : String(err);
		const timeout = msg.toLowerCase().includes('timeout');
		return { timeout, error: msg };
	}
}

// Playwright's call log ends with the reason the click could not land (covered, disabled, detached).
// Retry lines ("waiting 500ms", "retrying click action") carry no reason, so they are dropped.
const RETRY_LINE_RE = /- (waiting \d+ms|retrying\b)/;

function lastLogLine(message: string | undefined): string {
	const lines = (message ?? '')
		.split('\n')
		.map((l) => l.trim())
		.filter((l) => l && !RETRY_LINE_RE.test(l));
	return (lines.at(-1) ?? '').slice(0, 160);
}

function evaluateVerdict(
	errors: ActionErrors,
	clickTimeout: boolean,
	clickError: string | undefined,
	dialogNotClosable: boolean
): { verdict: 'pass' | 'warn' | 'fail'; detail: string } {
	const details: string[] = [];
	if (errors.pageErrors.length > 0) {
		details.push(`pageerror: ${errors.pageErrors.join('; ')}`);
	}
	if (errors.consoleErrors.length > 0) {
		details.push(`console: ${errors.consoleErrors.join('; ')}`);
	}
	if (errors.apiFailures.length > 0) {
		details.push(`api_fail: ${errors.apiFailures.join('; ')}`);
	}
	if (errors.externalRequests.length > 0) {
		details.push(`external: ${errors.externalRequests.join('; ')}`);
	}
	if (errors.destructiveRequests.length > 0) {
		details.push(`destructive request despite cancel: ${errors.destructiveRequests.join('; ')}`);
	}
	if (clickTimeout) {
		details.push(`click timeout (5s): ${lastLogLine(clickError)}`);
	}
	if (dialogNotClosable) {
		details.push('dialog-not-closable');
	}
	if (clickError && details.length === 0) {
		details.push(`click error: ${clickError}`);
	}

	if (details.length > 0) {
		return { verdict: 'fail', detail: details.join(' | ') };
	}
	if (errors.apiWarnings.length > 0) {
		return { verdict: 'warn', detail: `4xx: ${errors.apiWarnings.join('; ')}` };
	}
	return { verdict: 'pass', detail: '' };
}

async function discoverTabs(page: Page, route: string): Promise<DiscoveredTab[]> {
	const list = await page.evaluate((currentRoute) => {
		const found: Array<{ tab: string; url?: string; selector?: string }> = [];
		const tabLinks = Array.from(document.querySelectorAll<HTMLAnchorElement>('a[href*="tab="]'));
		for (const a of tabLinks) {
			if (!URL.canParse(a.href)) continue;
			const u = new URL(a.href);
			const param = u.searchParams.get('tab');
			// Tab links to another route are swept there, not here.
			if (param && u.pathname === currentRoute && !found.some((f) => f.tab === param)) {
				found.push({ tab: param, url: `${u.pathname}?tab=${param}` });
			}
		}

		const tabButtons = Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]'));
		for (const btn of tabButtons) {
			const id = btn.getAttribute('id') || '';
			const tabId = id.startsWith('tab-') ? id.slice(4) : '';
			const name = (
				btn.innerText ||
				btn.textContent ||
				btn.getAttribute('aria-label') ||
				''
			).trim();
			const identifier = tabId || name.toLowerCase().replace(/\s+/g, '-');
			if (identifier && !found.some((f) => f.tab === identifier)) {
				found.push({
					tab: identifier,
					url: `${currentRoute}?tab=${identifier}`,
					selector: id ? `#${id}` : undefined
				});
			}
		}
		return found;
	}, route);

	return list.slice(0, 20);
}

async function executeControlSweep(
	page: Page,
	url: string,
	tabSelector: string | undefined,
	ctrl: ControlDescriptor,
	errors: ActionErrors,
	route: string,
	tab: string
): Promise<SweepRow> {
	if (ctrl.isSkip) return skipRow(route, tab, ctrl, ctrl.skipReason ?? 'skip');

	await navigateAndSettle(page, url, tabSelector);
	resetErrors(errors);

	const resolved = await resolveControl(page, ctrl);
	if (!resolved)
		return skipRow(route, tab, ctrl, 'control gone on reload: saved state or live content changed');
	const { target, note } = resolved;
	if (target.isSkip) return skipRow(route, tab, ctrl, `${note}; ${target.skipReason ?? 'skip'}`);
	const loc = page.locator(CONTROL_QUERY).nth(target.domIndex);
	const { timeout: clickTimeout, error: clickError } = await interactControl(page, loc, target);
	await page.waitForTimeout(200);
	await attempt(settleToasts(page));

	if (ctrl.isDestructive || target.isDestructive) {
		await handleDestructiveCancel(page);
	}

	const dialogResult = await closeDialogIfOpen(page);
	const finalKind = dialogResult.opened ? 'dialog' : ctrl.kind;
	const dialogNotClosable = dialogResult.opened && !dialogResult.closed;

	await drainPending(errors);
	const verdictResult = evaluateVerdict(errors, clickTimeout, clickError, dialogNotClosable);
	const { verdict } = verdictResult;
	const detail = [note, verdictResult.detail].filter(Boolean).join('; ');
	const apiFailStr = [...errors.apiFailures, ...errors.apiWarnings].join('; ');
	const consoleStr = errors.consoleErrors.join('; ');

	return {
		route,
		tab,
		control: ctrl.key,
		kind: finalKind,
		verdict,
		api_fail: apiFailStr,
		console: consoleStr,
		detail
	};
}

async function sweepRoute(
	page: Page,
	route: string,
	tab: string,
	url: string,
	tabSelector: string | undefined,
	errors: ActionErrors,
	onRow: (row: SweepRow) => void
): Promise<void> {
	await navigateAndSettle(page, url, tabSelector);
	await drainPending(errors);
	const { verdict, detail } = evaluateVerdict(errors, false, undefined, false);
	const pageRow: SweepRow = {
		route,
		tab,
		control: '(page)',
		kind: 'page',
		verdict,
		api_fail: [...errors.apiFailures, ...errors.apiWarnings].join('; '),
		console: errors.consoleErrors.join('; '),
		detail
	};
	onRow(pageRow);

	const controls = await enumerateControls(page);
	const ordered = [
		...controls.filter((c) => !c.isDestructive),
		...controls.filter((c) => c.isDestructive)
	];

	for (const ctrl of ordered) {
		const row = await executeControlSweep(page, url, tabSelector, ctrl, errors, route, tab);
		onRow(row);
	}
}

test.describe('control-level app sweep', () => {
	test('planted negative: each failure signal turns the verdict to fail', () => {
		const clean = (): ActionErrors => ({
			pageErrors: [],
			consoleErrors: [],
			apiFailures: [],
			apiWarnings: [],
			externalRequests: [],
			destructiveRequests: [],
			pending: []
		});
		expect(evaluateVerdict(clean(), false, undefined, false).verdict).toBe('pass');
		const planted: Array<[keyof ActionErrors, string]> = [
			['pageErrors', 'TypeError: x is undefined'],
			['consoleErrors', 'Uncaught Error'],
			['apiFailures', 'GET /api/v1/instances (500)'],
			['externalRequests', 'https://sonarr.example.org/api/v3/series'],
			['destructiveRequests', 'DELETE /api/v1/instances/:id']
		];
		for (const [field, value] of planted) {
			const errors = clean();
			(errors[field] as string[]).push(value);
			expect(evaluateVerdict(errors, false, undefined, false).verdict, field).toBe('fail');
		}
		expect(evaluateVerdict(clean(), true, 'Timeout 5000ms', false).verdict).toBe('fail');
		expect(evaluateVerdict(clean(), false, undefined, true).verdict).toBe('fail');
		expect(isDestructiveRequest('DELETE', 'http://127.0.0.1:1/api/v1/instances/x')).toBe(true);
		expect(isDestructiveRequest('POST', 'http://127.0.0.1:1/api/v1/state/reset')).toBe(true);
		expect(isDestructiveRequest('POST', 'http://127.0.0.1:1/api/v1/instances/test')).toBe(false);
	});

	test('crawl all routes, tabs, and controls', async ({ page, context }) => {
		test.skip(!process.env.SNATCHARR_E2E_API, 'sweep runs only in API mode (SNATCHARR_E2E_API)');
		test.skip(
			process.env.SNATCHARR_E2E_SWEEP !== '1',
			'sweep runs only when SNATCHARR_E2E_SWEEP=1'
		);
		test.setTimeout(120 * 60 * 1000);

		const outPath = process.env.SNATCHARR_SWEEP_OUT || 'test-results/sweep.tsv';
		await fs.promises.mkdir(path.dirname(outPath), { recursive: true });
		await fs.promises.writeFile(
			outPath,
			'route\ttab\tcontrol\tkind\tverdict\tapi_fail\tconsole\tdetail\n',
			'utf-8'
		);

		const errors: ActionErrors = {
			pageErrors: [],
			consoleErrors: [],
			apiFailures: [],
			apiWarnings: [],
			externalRequests: [],
			destructiveRequests: [],
			pending: []
		};

		await setupNetworkGuard(context, errors);
		setupListeners(page, errors);

		const instanceRoutes = await fetchInstanceRoutes(page);
		const allRoutes = [...STATIC_ROUTES, ...instanceRoutes];
		const rows: SweepRow[] = [];

		const handleRow = (row: SweepRow) => {
			rows.push(row);
			fs.appendFileSync(outPath, `${formatRow(row)}\n`, 'utf-8');
		};

		for (const r of allRoutes) {
			resetErrors(errors);
			await sweepRoute(page, r, '', r, undefined, errors, handleRow);

			const tabs = await discoverTabs(page, r);
			for (const t of tabs) {
				const tabUrl = t.url ?? `${r}?tab=${t.tab}`;
				resetErrors(errors);
				await sweepRoute(page, r, t.tab, tabUrl, t.selector, errors, handleRow);
			}
		}

		const pagesCount = rows.filter((r) => r.control === '(page)').length;
		const controlsCount = rows.filter((r) => r.control !== '(page)').length;
		const failCount = rows.filter((r) => r.verdict === 'fail').length;
		const warnCount = rows.filter((r) => r.verdict === 'warn').length;
		const skipCount = rows.filter((r) => r.verdict === 'skip').length;

		console.log(
			`sweep pages ${pagesCount} controls ${controlsCount} fail ${failCount} warn ${warnCount} skip ${skipCount} soft ${softFailures.length}`
		);

		const failRows = rows.filter((r) => r.verdict === 'fail');
		if (failRows.length > 0) {
			const preview = failRows
				.slice(0, 10)
				.map((r) => `${r.route}\t${r.tab}\t${r.control}\t${r.verdict}\t${r.detail}`)
				.join('\n');
			expect(failRows.length, `sweep failed with ${failRows.length} failures:\n${preview}`).toBe(0);
		}
	});
});
