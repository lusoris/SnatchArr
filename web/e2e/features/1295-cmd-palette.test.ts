// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #1295

import { expect, test, type Locator, type Page } from '@playwright/test';

const pages = [
	{ name: 'Overview', path: '/' },
	{ name: 'Activity', path: '/activity' }
] as const;

/** The palette's option list; the page also renders native `<option>`s (language select). */
function commandList(page: Page): Locator {
	return page.getByRole('listbox', { name: 'Commands' });
}

function optionTitles(page: Page): Locator {
	return commandList(page).getByRole('option').locator('.ssentio-cmd__title');
}

async function openPalette(page: Page, path: string): Promise<void> {
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.goto(path);
	await page.waitForLoadState('networkidle');
	await page.keyboard.press('Control+k');
	await expect(page.locator('#ssentio-cmd-input')).toBeFocused();
	await expect(commandList(page).getByRole('option').first()).toBeVisible();
}

/** Labels of options that print the same text as title and subtitle (`X — X`). */
async function doubledLabels(page: Page): Promise<string[]> {
	const labels = await optionTitles(page).allInnerTexts();
	return labels.filter((label) => /^(.+) — \1$/u.test(label.trim()));
}

for (const target of pages) {
	test.describe(`search palette on ${target.name}`, () => {
		test('positive: Ctrl+K lists every command with a title', async ({ page }) => {
			await openPalette(page, target.path);
			const labels = await optionTitles(page).allInnerTexts();
			expect(labels.length).toBeGreaterThan(1);
			expect(labels.filter((label) => label.trim() === '')).toEqual([]);
		});

		test('negative: no option repeats its title as subtitle', async ({ page }) => {
			await openPalette(page, target.path);
			expect(await doubledLabels(page)).toEqual([]);
		});

		test('boundary: a query matching one page leaves its label shown once', async ({ page }) => {
			await openPalette(page, target.path);
			await page.keyboard.type('Account');
			await expect(optionTitles(page).first()).toHaveText('Account');
			expect(await doubledLabels(page)).toEqual([]);
		});
	});
}
