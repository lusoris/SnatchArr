// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { defineConfig } from '@playwright/test';

const e2eApi = process.env.SNATCHARR_E2E_API;
const authFile = 'e2e/.auth/admin.json';

export default defineConfig({
	testDir: 'e2e',
	fullyParallel: true,
	workers: 2,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	reporter: process.env.CI ? 'github' : 'list',
	expect: { timeout: 15_000 },
	use: { baseURL: 'http://127.0.0.1:4173', trace: 'retain-on-failure' },
	webServer: {
		command: 'pnpm build && pnpm preview --host 127.0.0.1 --port 4173',
		url: 'http://127.0.0.1:4173',
		reuseExistingServer: !process.env.CI && !e2eApi,
		timeout: 180_000,
		env: e2eApi ? { SNATCHARR_API: e2eApi } : undefined
	},
	projects: !e2eApi
		? [{ name: 'chromium', testMatch: /a11y\.test\.ts/, use: { browserName: 'chromium' } }]
		: [
				{
					name: 'public',
					testMatch: /a11y\.test\.ts/,
					grep: /public:|routes:/,
					use: { browserName: 'chromium' }
				},
				{
					name: 'auth-setup',
					testMatch: /auth\.setup\.ts/,
					dependencies: ['public'],
					use: { browserName: 'chromium' }
				},
				{
					name: 'anonymous',
					testMatch: /a11y\.test\.ts/,
					grep: /anonymous:/,
					dependencies: ['auth-setup'],
					use: { browserName: 'chromium' }
				},
				{
					name: 'authenticated',
					testMatch: /a11y\.test\.ts/,
					grep: /authenticated:/,
					dependencies: ['auth-setup'],
					use: { browserName: 'chromium', storageState: authFile }
				},
				{
					name: 'flow',
					testMatch: /flow\.test\.ts/,
					dependencies: ['authenticated'],
					use: { browserName: 'chromium', storageState: authFile }
				}
			]
});
