// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #879 #890
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { VIEWPORTS, assertCardScrollsToLast, assertScrollBudget } from '../layout.js';
import {
	createInstance,
	createRow,
	deleteRows,
	requireEnv,
	signIn,
	uniqueSuffix
} from './layout-fixtures.js';

const execFileAsync = promisify(execFile);
const FLEET_SIZE = 8;
const SCHEDULE_COUNT = 5;
const RUN_COUNT = 7;
// Rows a dashboard card shows before its list scrolls: the caps in web/src/lib/overview.ts.
const FLEET_CAP = 5;
const SCHEDULES_CAP = 3;
// ActivityFeed bounds the instance log to 16rem and RunList its table to 11rem from md up.
const LOG_MAX_PX = 16 * 16;
const RUN_LIST_MAX_PX = 11 * 16;
const PHONE_RUN_CARDS = 5;

function card(page: Page, title: string): Locator {
	return page.getByRole('region', { name: `${title}, scrollable list`, exact: true });
}

// The name in the card's last row, read from the page: other tests add rows to the same lists.
async function lastRowName(body: Locator, title: string): Promise<string> {
	await expect(body, `${title}: no scrolling region`).toBeVisible();
	const link = body.locator('tbody > tr, li').last().getByRole('link').first();
	return (await link.innerText()).trim();
}

async function expectCardKeepsRows(
	page: Page,
	title: string,
	names: readonly string[],
	cap: number
): Promise<void> {
	const body = card(page, title);
	await expect(body, `${title}: no scrolling region`).toBeVisible();
	expect(names.length, 'the test seeds more rows than the card shows').toBeGreaterThan(cap);
	// Every seeded row is in the card, the ones past the cap included.
	for (const name of names) {
		await expect(
			body.getByText(name, { exact: true }).first(),
			`${title}: row ${name} missing`
		).toBeAttached();
	}
	await assertCardScrollsToLast(page, title, await lastRowName(body, title));
}

// Fills `ids` as it goes, so a failed create leaves the earlier rows for the caller to delete.
async function seedSchedules(
	page: Page,
	csrf: string,
	names: readonly string[],
	ids: string[]
): Promise<void> {
	for (const name of names) {
		// A short window that raises the cap, on one weekday: it changes nothing for other tests.
		const id = await createRow(page, csrf, 'schedules', {
			name,
			days: [1],
			start: '03:00',
			end: '03:05',
			tz: 'UTC',
			action: 'cap_override',
			cap_value: 500,
			enabled: true
		});
		ids.push(id);
	}
}

test('890: every capped dashboard card scrolls to all of its rows', async ({ page }) => {
	const csrf = await signIn(page);
	const prefix = `e2e-890-${uniqueSuffix()}`;
	const instanceNames = Array.from({ length: FLEET_SIZE }, (_, i) => `${prefix}-i${i + 1}`);
	const scheduleNames = Array.from({ length: SCHEDULE_COUNT }, (_, i) => `${prefix}-s${i + 1}`);
	const instanceIds: string[] = [];
	const scheduleIds: string[] = [];
	try {
		for (const name of instanceNames) instanceIds.push(await createInstance(page, csrf, name));
		await seedSchedules(page, csrf, scheduleNames, scheduleIds);

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto('/');
		await expectCardKeepsRows(page, 'Instance fleet', instanceNames, FLEET_CAP);
		await expectCardKeepsRows(page, 'Schedules', scheduleNames, SCHEDULES_CAP);

		// With every card holding its rows, the dashboard still fits the 1280x800 scroll budget (#879).
		await assertScrollBudget(page, '/');
	} finally {
		await deleteRows(page, csrf, 'schedules', scheduleIds);
		await deleteRows(page, csrf, 'instances', instanceIds);
	}
});

// Queues a missing-kind run and lets the fake worker finish it, so the instance gains one run.
async function completeRuns(
	page: Page,
	csrf: string,
	instanceId: string,
	count: number
): Promise<void> {
	const bin = requireEnv('SNATCHARR_E2E_FAKEWORKER');
	const grpc = process.env.SNATCHARR_E2E_GRPC || '127.0.0.1:19090';
	const token = process.env.SNATCHARR_E2E_WORKER_TOKEN || 'smoke-worker-token';
	for (let i = 0; i < count; i++) {
		const queued = await page.request.post(`/api/v1/instances/${instanceId}/runs`, {
			headers: { 'X-CSRF-Token': csrf },
			data: { kind: 'missing' }
		});
		expect(queued.status(), 'a new run is queued').toBe(201);
		await execFileAsync(bin, [
			...['-grpc', grpc, '-worker-id', `worker-879-${instanceId.slice(0, 8)}`],
			...['-instance', instanceId, '-outcome', 'done', '-count', '1', '-timeout', '10s'],
			...['-token', token]
		]);
	}
}

async function expectDesktopActivityBounded(page: Page, route: string): Promise<void> {
	const runs = page.locator('[role="region"]:has(table.run-cards)');
	await expect(runs).toBeVisible();
	await expect(runs, 'the run list takes keyboard focus to scroll').toHaveAttribute(
		'tabindex',
		'0'
	);
	const size = await runs.evaluate((el) => ({ h: el.clientHeight, content: el.scrollHeight }));
	expect(size.h, 'run list stays inside its 11rem box').toBeLessThanOrEqual(RUN_LIST_MAX_PX + 2);
	expect(size.content, 'the longer run list scrolls inside its box').toBeGreaterThan(size.h);

	const log = page.getByRole('log');
	await expect(log).toBeVisible();
	const logHeight = (await log.boundingBox())!.height;
	expect(logHeight, 'the log box is bounded to 16rem').toBeLessThanOrEqual(LOG_MAX_PX + 2);
	await assertScrollBudget(page, route);
}

async function expectPhoneRunCards(page: Page): Promise<void> {
	const region = page.locator('[role="region"]:has(table.run-cards)');
	const cards = region.locator('tbody > tr').filter({ visible: true });
	await expect(cards, 'a phone shows five compact run cards').toHaveCount(PHONE_RUN_CARDS);
	const { scrollHeight, innerHeight } = await page.evaluate(() => ({
		scrollHeight: document.documentElement.scrollHeight,
		innerHeight: window.innerHeight
	}));
	expect(scrollHeight, 'the phone page stays within two screens').toBeLessThanOrEqual(
		2 * innerHeight
	);

	const toggle = region.locator('xpath=following-sibling::button[@aria-expanded]');
	await expect(toggle).toHaveAttribute('aria-expanded', 'false');
	await toggle.click();
	await expect(toggle).toHaveAttribute('aria-expanded', 'true');
	await expect(cards).toHaveCount(RUN_COUNT);
}

test('879: instance activity bounds its run list and log on desktop and its run cards on a phone', async ({
	page
}) => {
	const csrf = await signIn(page);
	const instanceId = await createInstance(page, csrf, `e2e-879-${uniqueSuffix()}`);
	const route = `/instances/${instanceId}/activity`;
	try {
		await completeRuns(page, csrf, instanceId, RUN_COUNT);

		await page.setViewportSize(VIEWPORTS.desktop);
		await page.goto(route);
		await expectDesktopActivityBounded(page, '/instances/[id]/activity');

		await page.setViewportSize(VIEWPORTS.phone);
		await page.goto(route);
		await expectPhoneRunCards(page);
	} finally {
		await deleteRows(page, csrf, 'instances', [instanceId]);
	}
});
