// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { expect, test } from '@playwright/test';
import { mockSettingsApi } from './mocks.js';
import { settleToasts } from './settle.js';

test('settleToasts: waits for a mid-transition toast to reach full opacity', async ({ page }) => {
	await mockSettingsApi(page);
	await page.goto('/system');

	const saveBtn = page.getByRole('button', { name: /Save|Speichern/i }).first();
	await expect(saveBtn).toBeVisible({ timeout: 5000 });

	// Trigger a success toast
	await saveBtn.click();

	const toast = page.locator('[data-sonner-toast]').first();
	await expect(toast).toBeAttached({ timeout: 3000 });

	// Right after trigger, the toast is mid-transition: opacity < 1
	const initialOpacity = await toast.evaluate((el) => window.getComputedStyle(el).opacity);
	expect(Number.parseFloat(initialOpacity)).toBeLessThan(1);

	// Settle toasts
	await settleToasts(page);

	// After settling, computed opacity must be exactly 1
	const settledOpacity = await toast.evaluate((el) => window.getComputedStyle(el).opacity);
	expect(settledOpacity).toBe('1');
});

test('settleToasts: resolves immediately when no toasts exist', async ({ page }) => {
	await page.goto('/login');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await settleToasts(page);
});
