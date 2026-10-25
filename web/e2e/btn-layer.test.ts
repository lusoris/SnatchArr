// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSignedInApi } from './mocks.js';

test.use({
	viewport: { width: 1280, height: 800 }
});

test('app.css component and utility layers', async ({ page }) => {
	await mockSignedInApi(page);
	await page.goto('/activity');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	// Responsive utility md:hidden on Filters toggle button must override .btn at md+ (1280px)
	const toggleBtn = page.getByRole('button', { name: 'Filters', exact: true, includeHidden: true });
	await expect(toggleBtn).toBeAttached();
	const toggleDisplay = await toggleBtn.evaluate((el) => window.getComputedStyle(el).display);
	expect(toggleDisplay).toBe('none');

	// .btn.btn-sm element must have computed min-height 32px
	const btnSm = page.locator('.btn.btn-sm').first();
	await expect(btnSm).toBeAttached();
	const minHeight = await btnSm.evaluate((el) => window.getComputedStyle(el).minHeight);
	expect(minHeight).toBe('32px');

	// Padding utility p-3 on form.card must override .card padding (1rem / 16px -> 0.75rem / 12px)
	const card = page.locator('form.card');
	await expect(card).toBeAttached();
	const cardPadding = await card.evaluate((el) => window.getComputedStyle(el).paddingTop);
	expect(cardPadding).toBe('12px');

	// Padding utility py-1 on search input must override .input padding (0.5rem / 8px -> 0.25rem / 4px)
	const searchInput = page.locator('input#filter-q');
	await expect(searchInput).toBeAttached();
	const inputPaddingTop = await searchInput.evaluate(
		(el) => window.getComputedStyle(el).paddingTop
	);
	expect(inputPaddingTop).toBe('4px');
});
