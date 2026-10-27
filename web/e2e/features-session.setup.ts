// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import path from 'node:path';
import { expect, test as setup } from '@playwright/test';

// The feature tests run beside e2e/flow.test.ts, which logs the shared admin session out.
// They sign in with a session of their own, so that logout cannot end it.
const authFile = path.resolve(import.meta.dirname, '.auth', 'features.json');

setup('sign in a session for the feature tests', async ({ page }) => {
	const res = await page.request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'smoke-password-1234' }
	});
	expect(res.ok(), `login status ${res.status()}`).toBeTruthy();
	const session = await page.request.get('/api/v1/auth/session');
	expect(session.ok(), `session status ${session.status()}`).toBeTruthy();
	await page.context().storageState({ path: authFile });
});
