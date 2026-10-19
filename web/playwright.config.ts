// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { defineConfig } from '@playwright/test';

const e2eApi = process.env.SNATCHARR_E2E_API;
const authFile = 'e2e/.auth/admin.json';
// Visual baseline (tools/e2e/web_visual.sh): runs only inside the pinned Playwright image.
const visual = !!process.env.SNATCHARR_VISUAL;
const visualProject = {
	name: 'visual',
	testMatch: /visual\.test\.ts/,
	// The comparison freezes animations and plants its diffs with inline style tags, which the
	// page CSP refuses; e2e/security.test.ts holds the policy itself.
	use: {
		browserName: 'chromium' as const,
		viewport: { width: 1280, height: 800 },
		bypassCSP: true
	}
};

export default defineConfig({
	testDir: 'e2e',
	fullyParallel: true,
	workers: 2,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI && !visual ? 1 : 0,
	reporter: process.env.CI ? 'github' : 'list',
	expect: {
		timeout: 15_000,
		toHaveScreenshot: { maxDiffPixels: 200, animations: 'disabled', caret: 'hide', scale: 'css' }
	},
	snapshotPathTemplate: `${process.env.SNATCHARR_VISUAL_DIR ?? 'e2e/visual'}/{arg}{ext}`,
	outputDir: process.env.SNATCHARR_VISUAL_OUT ?? 'test-results',
	use: { baseURL: 'http://127.0.0.1:4173', trace: 'retain-on-failure' },
	webServer: {
		command: 'pnpm build && pnpm preview --host 127.0.0.1 --port 4173',
		url: 'http://127.0.0.1:4173',
		reuseExistingServer: !process.env.CI && !e2eApi,
		timeout: 180_000,
		env: e2eApi ? { SNATCHARR_API: e2eApi } : undefined
	},
	projects: visual
		? [visualProject]
		: !e2eApi
			? [
					{
						name: 'chromium',
						testMatch: /(a11y|theme|security|nonblocking|keyboard-nav|inp|instance-tabs)\.test\.ts/,
						use: { browserName: 'chromium' }
					}
				]
			: [
					{
						name: 'security',
						testMatch: /security\.test\.ts/,
						use: { browserName: 'chromium' }
					},
					{
						name: 'public',
						testMatch: /(a11y|theme)\.test\.ts/,
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
						testMatch: /(a11y|phone-overflow|nonblocking|keyboard-nav|inp|instance-tabs)\.test\.ts/,
						grep: /authenticated:|nonblocking:/,
						dependencies: ['auth-setup'],
						use: { browserName: 'chromium', storageState: authFile }
					},
					{
						name: 'flow',
						testMatch: /(^|\/)flow\.test\.ts/,
						dependencies: ['authenticated'],
						use: { browserName: 'chromium', storageState: authFile }
					}
				]
});
