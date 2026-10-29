// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test, type Page } from '@playwright/test';
import {
	json,
	mockAuthSetupRoutes,
	mockInstanceNotFound,
	mockInstanceQueueApi,
	samplePolicyData
} from './mocks.js';

const instanceId = '00000000-0000-0000-0000-000000000001';
const unknownId = '00000000-0000-0000-0000-000000000404';

async function setupAuthMocks(page: Page) {
	await mockAuthSetupRoutes(page);
}

async function setupInstanceDetailMocks(page: Page) {
	await page.route(`**/api/v1/instances/${instanceId}`, (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				id: instanceId,
				name: 'Library Sonarr',
				kind: 'sonarr',
				base_url: 'http://sonarr.local:8989',
				source: 'manual',
				enabled: true,
				created_at: '2026-01-01T00:00:00Z',
				updated_at: '2026-01-01T00:00:00Z'
			})
		})
	);
	await mockInstanceNotFound(page, unknownId);
}

async function setupInstancePolicyMocks(page: Page) {
	await page.route(`**/api/v1/instances/${instanceId}/policy`, (route) =>
		route.fulfill(json(samplePolicyData))
	);
	await page.route(`**/api/v1/instances/${instanceId}/policy/filter-options`, (route) =>
		route.fulfill(json({ tags: [], quality_profiles: [] }))
	);
}

async function setupQueueMocks(page: Page) {
	await mockInstanceQueueApi(page, instanceId);
	await page.route('**/api/v1/runs?**', (route) =>
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([])
		})
	);
}

async function setupMocks(page: Page) {
	await setupAuthMocks(page);
	await setupInstanceDetailMocks(page);
	await setupInstancePolicyMocks(page);
	await setupQueueMocks(page);
}

test.describe('instance detail subtabs', () => {
	test.beforeEach(async ({ page }) => {
		await setupMocks(page);
	});

	test('click each tab -> URL changes, aria-current moves, heading of tab visible', async ({
		page
	}) => {
		await page.goto(`/instances/${instanceId}`);
		await expect(page.getByRole('heading', { level: 1, name: 'Library Sonarr' })).toBeVisible();

		const tabNav = page.getByRole('navigation', {
			name: /Instance tabs|Instanz-Bereiche|Instance sections/i
		});
		await expect(tabNav).toBeVisible();

		// Overview active initially
		const overviewTab = tabNav.getByRole('link', { name: /Overview|Übersicht/i });
		await expect(overviewTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Edit instance|Instanz bearbeiten/i })
		).toBeVisible();

		// Policy tab
		const policyTab = tabNav.getByRole('link', { name: /Policy|Richtlinie|Snatch-Regeln/i });
		await policyTab.click();
		await expect(page).toHaveURL(`/instances/${instanceId}/policy`);
		await expect(policyTab).toHaveAttribute('aria-current', 'page');
		await expect(overviewTab).not.toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Snatch policy|Snatch-Regeln/i })
		).toBeVisible();

		// Queue tab
		const queueTab = tabNav.getByRole('link', { name: /Queue|Warteschlange/i });
		await queueTab.click();
		await expect(page).toHaveURL(`/instances/${instanceId}/queue`);
		await expect(queueTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Queue management|Warteschlangenverwaltung/i })
		).toBeVisible();

		// Activity tab
		const activityTab = tabNav.getByRole('link', { name: /Activity|Aktivität/i });
		await activityTab.click();
		await expect(page).toHaveURL(`/instances/${instanceId}/activity`);
		await expect(activityTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Preview|Probelauf-Vorschau/i })
		).toBeVisible();

		// Memory tab
		const memoryTab = tabNav.getByRole('link', { name: /Memory|Speicher/i });
		await memoryTab.click();
		await expect(page).toHaveURL(`/instances/${instanceId}/memory`);
		await expect(memoryTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Processed memory|Speicher/i })
		).toBeVisible();
	});

	test('browser back returns to previous tab', async ({ page }) => {
		await page.goto(`/instances/${instanceId}`);
		const tabNav = page.getByRole('navigation', {
			name: /Instance tabs|Instanz-Bereiche|Instance sections/i
		});
		await expect(tabNav).toBeVisible();

		const policyTab = tabNav.getByRole('link', { name: /Policy|Richtlinie|Snatch-Regeln/i });
		await policyTab.click();
		await expect(page).toHaveURL(`/instances/${instanceId}/policy`);
		await expect(policyTab).toHaveAttribute('aria-current', 'page');

		await page.goBack();
		await expect(page).toHaveURL(`/instances/${instanceId}`);
		const overviewTab = tabNav.getByRole('link', { name: /Overview|Übersicht/i });
		await expect(overviewTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Edit instance|Instanz bearbeiten/i })
		).toBeVisible();
	});

	test('reload on /instances/<id>/queue keeps the queue tab', async ({ page }) => {
		await page.goto(`/instances/${instanceId}/queue`);
		const tabNav = page.getByRole('navigation', {
			name: /Instance tabs|Instanz-Bereiche|Instance sections/i
		});
		const queueTab = tabNav.getByRole('link', { name: /Queue|Warteschlange/i });
		await expect(queueTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Queue management|Warteschlangenverwaltung/i })
		).toBeVisible();

		await page.reload();
		await expect(page).toHaveURL(`/instances/${instanceId}/queue`);
		await expect(queueTab).toHaveAttribute('aria-current', 'page');
		await expect(
			page.getByRole('heading', { level: 2, name: /Queue management|Warteschlangenverwaltung/i })
		).toBeVisible();
	});

	test('unknown id (404) shows the not-found text and a link to /instances', async ({ page }) => {
		await page.goto(`/instances/${unknownId}`);
		await expect(
			page.getByRole('heading', { level: 1, name: /Instance not found|Instanz nicht gefunden/i })
		).toBeVisible();
		const backLink = page.getByRole('link', {
			name: /Back to instances|Zurück zu Instanzen/i
		});
		await expect(backLink).toBeVisible();
		await expect(backLink).toHaveAttribute('href', '/instances');

		// Header, tabs and normal children are not rendered
		const tabNav = page.getByRole('navigation', {
			name: /Instance tabs|Instanz-Bereiche|Instance sections/i
		});
		await expect(tabNav).not.toBeVisible();
	});

	test('at 390x844 document.scrollingElement.scrollWidth <= innerWidth on every tab', async ({
		page
	}) => {
		await page.setViewportSize({ width: 390, height: 844 });
		const subroutes = ['', '/policy', '/queue', '/activity', '/memory'];

		for (const subroute of subroutes) {
			await page.goto(`/instances/${instanceId}${subroute}`);
			await page.waitForLoadState('networkidle');

			const { scrollWidth, innerWidth } = await page.evaluate(() => ({
				scrollWidth: document.scrollingElement?.scrollWidth ?? document.documentElement.scrollWidth,
				innerWidth: window.innerWidth
			}));

			expect(
				scrollWidth,
				`Subroute ${subroute} has horizontal overflow at 390px: ${scrollWidth} > ${innerWidth}`
			).toBeLessThanOrEqual(innerWidth);
		}
	});

	test('at 1280x800 the sidebar and the instance header stay in view after scrolling the policy tab', async ({
		page
	}) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await page.goto(`/instances/${instanceId}/policy`);
		await page.waitForLoadState('networkidle');
		const scrolled = await page.evaluate(() => {
			window.scrollTo(0, document.documentElement.scrollHeight);
			return window.scrollY;
		});
		expect(scrolled, 'policy tab must be taller than the viewport').toBeGreaterThan(0);

		const sidebar = await page.locator('aside').first().boundingBox();
		expect(sidebar?.y).toBe(0);
		expect(sidebar?.height).toBe(800);

		const header = await page.locator('header', { has: page.locator('h1') }).boundingBox();
		expect(header?.y).toBe(0);
	});
});
