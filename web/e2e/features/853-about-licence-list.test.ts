// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #853
import { expect, test, type Locator, type Page } from '@playwright/test';
import { VIEWPORTS, assertScrollBudget } from '../layout.js';
import { signIn } from './layout-fixtures.js';

interface LicencePackage {
	ecosystem: 'go' | 'rust' | 'npm';
	name: string;
	licence: string;
}

const ECOSYSTEMS = ['go', 'rust', 'npm'] as const;

async function readPackages(page: Page): Promise<LicencePackage[]> {
	const res = await page.request.get('/licences.json');
	expect(res.ok(), `licences.json status ${res.status()}`).toBeTruthy();
	return ((await res.json()) as { packages: LicencePackage[] }).packages;
}

// The same rule the filter field applies: the query matches a package name or its licence.
function matching(packages: readonly LicencePackage[], query: string): number {
	const q = query.toLowerCase();
	return packages.filter(
		(p) => p.name.toLowerCase().includes(q) || p.licence.toLowerCase().includes(q)
	).length;
}

async function expectBoundedScrollRegion(region: Locator): Promise<void> {
	await expect(region).toHaveAttribute('tabindex', '0');
	const sizes = await region.evaluate((el) => ({
		scrollHeight: el.scrollHeight,
		clientHeight: el.clientHeight,
		maxHeight: parseFloat(getComputedStyle(el).maxHeight)
	}));
	expect(sizes.maxHeight, 'the list has a height limit').toBeGreaterThan(0);
	expect(sizes.clientHeight, 'the list stays inside its limit').toBeLessThanOrEqual(
		sizes.maxHeight
	);
	expect(sizes.scrollHeight, 'the long list scrolls inside its box').toBeGreaterThan(
		sizes.clientHeight
	);
}

test('853: the About licence list fits one screen, splits by ecosystem and filters', async ({
	page
}) => {
	await signIn(page);
	const packages = await readPackages(page);
	// Ecosystems that hold packages, in tab order, with their packages.
	const groups = ECOSYSTEMS.map((id) => ({
		id,
		list: packages.filter((p) => p.ecosystem === id)
	})).filter((g) => g.list.length > 0);
	expect(groups.length, 'the served licence file lists several ecosystems').toBeGreaterThan(1);

	await page.setViewportSize(VIEWPORTS.desktop);
	await page.goto('/system?tab=about');
	const filter = page.locator('#third-party-filter');
	await expect(filter).toBeVisible();

	// The whole page, every package included, stays inside the 1280x800 scroll budget.
	await assertScrollBudget(page, '/system?tab=about');

	// One tab per ecosystem with packages; the first lists every package of its ecosystem.
	const region = page.locator('main [role="region"][tabindex="0"]');
	await expect(page.locator(ECOSYSTEMS.map((id) => `#tab-${id}`).join(', '))).toHaveCount(
		groups.length
	);
	await expectBoundedScrollRegion(region);
	await expect(region.locator('li')).toHaveCount(groups[0].list.length);

	// The filter narrows the list by name or licence, and an unmatched query empties it.
	const probe = groups[0].list[0].name;
	await filter.fill(probe);
	await expect(region.locator('li')).toHaveCount(matching(groups[0].list, probe));
	await filter.fill('no-such-package-853-xyz');
	await expect(region).toHaveCount(0);

	// Another tab lists the packages of its own ecosystem.
	await filter.fill('');
	const last = groups[groups.length - 1];
	await page.locator(`#tab-${last.id}`).click();
	await expect(region.locator('li')).toHaveCount(last.list.length);
});
