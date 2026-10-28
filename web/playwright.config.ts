// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { defineConfig } from '@playwright/test';

const e2eApi = process.env.SNATCHARR_E2E_API;
const authFile = 'e2e/.auth/admin.json';
// Own session for the feature tests; flow.test.ts logs the admin session out (features-session.setup.ts).
const featuresAuthFile = 'e2e/.auth/features.json';
const e2eReport = process.env.SNATCHARR_E2E_REPORT;
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
// Served-site layout check (e2e/layout.test.ts): runs against a deployed SnatchArr with a
// signed-in storage state the operator exports; no local server starts.
const servedUrl = process.env.SNATCHARR_SERVED_URL;
const servedState = process.env.SNATCHARR_SERVED_STATE;
if (servedUrl && !servedState) {
	throw new Error('SNATCHARR_SERVED_STATE is required with a served URL');
}
const servedProject = {
	name: 'served-layout',
	testMatch: /layout\.test\.ts/,
	use: {
		browserName: 'chromium' as const,
		baseURL: servedUrl,
		storageState: servedState
	}
};

export default defineConfig({
	testDir: 'e2e',
	fullyParallel: true,
	workers: 2,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI && !visual ? 1 : 0,
	// The release check (tools/ci/release_e2e_check.py) reads this report to tell that every
	// project ran and passed on the commit it names.
	reporter: e2eReport
		? [[process.env.CI ? 'github' : 'list'], ['json', { outputFile: e2eReport }]]
		: process.env.CI
			? 'github'
			: 'list',
	metadata: { commit: process.env.SNATCHARR_E2E_COMMIT ?? '' },
	expect: {
		timeout: 15_000,
		toHaveScreenshot: { maxDiffPixels: 200, animations: 'disabled', caret: 'hide', scale: 'css' }
	},
	snapshotPathTemplate: `${process.env.SNATCHARR_VISUAL_DIR ?? 'e2e/visual'}/{arg}{ext}`,
	outputDir: process.env.SNATCHARR_VISUAL_OUT ?? 'test-results',
	use: { baseURL: 'http://127.0.0.1:4173', trace: 'retain-on-failure' },
	webServer: servedUrl
		? undefined
		: {
				command: 'pnpm build && pnpm preview --host 127.0.0.1 --port 4173',
				url: 'http://127.0.0.1:4173',
				reuseExistingServer: !process.env.CI && !e2eApi,
				timeout: 180_000,
				env: e2eApi ? { SNATCHARR_API: e2eApi } : undefined
			},
	projects: servedUrl
		? [servedProject]
		: visual
			? [visualProject]
			: !e2eApi
				? [
						{
							name: 'chromium',
							testMatch:
								/(a11y|theme|btn-layer|security|nonblocking|keyboard-nav|inp|instance-tabs|entity-links|app-logos|session-expiry|live|modules|settle|jellyfin-signin)\.test\.ts/,
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
							testMatch:
								/(a11y|phone-overflow|layout|nonblocking|keyboard-nav|inp|instance-tabs|entity-links|app-logos|redirects)\.test\.ts/,
							grep: /authenticated:|nonblocking:/,
							dependencies: ['auth-setup'],
							use: { browserName: 'chromium', storageState: authFile }
						},
						{
							name: 'flow',
							testMatch: /(^|\/)flow\.test\.ts/,
							dependencies: ['authenticated'],
							use: { browserName: 'chromium', storageState: authFile }
						},
						// One file per released feat or fix, named <pr>-<slug>.test.ts, with a
						// `covers: #<pr>` line; tools/ci/release_e2e_check.py maps release commits to them.
						{
							name: 'features-auth',
							testMatch: /features-session\.setup\.ts/,
							dependencies: ['authenticated'],
							use: { browserName: 'chromium' }
						},
						{
							name: 'features',
							testMatch: /(^|\/)features\/[^/]+\.test\.ts/,
							testIgnore: /(^|\/)features\/\d+-adult-[^/]+\.test\.ts/,
							dependencies: ['features-auth'],
							use: { browserName: 'chromium', storageState: featuresAuthFile }
						},
						// Adult feature tests share one module switch and one global sync, so
						// they run one at a time: <pr>-adult-<slug>.test.ts.
						{
							name: 'features-adult',
							testMatch: /(^|\/)features\/\d+-adult-[^/]+\.test\.ts/,
							dependencies: ['features-auth'],
							workers: 1,
							use: { browserName: 'chromium', storageState: featuresAuthFile }
						}
					]
});
