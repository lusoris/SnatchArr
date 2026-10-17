// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Browser-side web security cases. The case lists (CSP weaknesses, output encoding contexts)
// follow s4ntrx/Cybersecurity-Threats-Guide 02-web-application-security at commit
// 34c9097f8d40f573b8ff10612813424c6711d574 (MIT): xss-attacks/prevention/csp_analyzer.py and
// output_encoding.py; REUSE.toml carries their MIT notice. The server-side cases (CSRF,
// headers, session cookie) live in api/internal/app/websecurity_integration_test.go.
import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page, type Response } from '@playwright/test';

/** Weaknesses csp_analyzer.py reports, one message per finding; [] means the policy passes. */
export function cspWeaknesses(policy: string): string[] {
	const directives = new Map<string, string[]>();
	for (const part of policy.split(';')) {
		const [name, ...sources] = part.trim().split(/\s+/);
		if (name && !directives.has(name.toLowerCase())) directives.set(name.toLowerCase(), sources);
	}
	const issues: string[] = [];
	const script = directives.get('script-src') ?? directives.get('default-src');
	if (!script) issues.push('no script-src or default-src');
	for (const token of script ?? []) {
		if (token === "'unsafe-inline'" || token === "'unsafe-eval'")
			issues.push(`script-src ${token}`);
		if (['*', 'http:', 'https:', 'data:'].includes(token) || token.startsWith('*.')) {
			issues.push(`script-src source ${token} is too broad`);
		}
	}
	for (const required of ['object-src', 'base-uri']) {
		if (!directives.has(required)) issues.push(`no ${required}`);
	}
	return issues;
}

/** Records every CSP violation the page reports, from the first byte of each document. */
async function recordViolations(page: Page): Promise<() => Promise<string[]>> {
	await page.addInitScript(() => {
		const seen: string[] = [];
		(window as unknown as { __cspViolations: string[] }).__cspViolations = seen;
		document.addEventListener('securitypolicyviolation', (e) => {
			seen.push(
				`${e.effectiveDirective} blocked ${e.blockedURI || 'inline'} at ${e.sourceFile}:${e.lineNumber}:${e.columnNumber} ${e.sample}`
			);
		});
	});
	return () =>
		page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations);
}

/** The policy a response enforces: the header (preview) or the meta tag (static build). */
async function pagePolicy(page: Page, response: Response | null): Promise<string> {
	const header = response?.headers()['content-security-policy'];
	if (header) return header;
	const meta = page.locator('meta[http-equiv="content-security-policy"]');
	await expect(meta).toHaveCount(1);
	return (await meta.getAttribute('content')) ?? '';
}

test('csp: the analyser flags the weak policies from the case list', () => {
	expect(cspWeaknesses("script-src 'unsafe-inline' https:")).toEqual([
		"script-src 'unsafe-inline'",
		'script-src source https: is too broad',
		'no object-src',
		'no base-uri'
	]);
	expect(cspWeaknesses("default-src 'self'; script-src 'self' 'unsafe-eval'")).toContain(
		"script-src 'unsafe-eval'"
	);
	expect(cspWeaknesses("img-src 'self'")).toContain('no script-src or default-src');
	expect(
		cspWeaknesses(
			"default-src 'self'; script-src 'self' 'sha256-x'; object-src 'none'; base-uri 'none'"
		)
	).toEqual([]);
});

test('csp: the static fallback page the API embeds carries the policy as a meta tag', () => {
	const html = fs.readFileSync(path.resolve(import.meta.dirname, '../build/index.html'), 'utf-8');
	const policy = /<meta http-equiv="content-security-policy" content="([^"]+)"/.exec(html)?.[1];
	expect(policy).toBeDefined();
	expect(cspWeaknesses(policy ?? '')).toEqual([]);
});

const PUBLIC_ROUTES = process.env.SNATCHARR_E2E_API ? ['/setup'] : ['/login', '/setup'];

for (const route of PUBLIC_ROUTES) {
	test(`csp: ${route} ships a strict policy and the app runs under it`, async ({ page }) => {
		const violations = await recordViolations(page);
		const response = await page.goto(route);
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		expect(cspWeaknesses(await pagePolicy(page, response))).toEqual([]);
		expect(await violations()).toEqual([]);
	});
}

if (!process.env.SNATCHARR_E2E_API) {
	// output_encoding.py contexts: HTML text (a tag, an attribute breakout, a script element)
	// and a URL with a javascript: scheme. Svelte must render all of them as inert text.
	const hostileName = '<img src=x onerror="window.__xss=1">"\'><script>window.__xss=2</script>';
	const hostileUrl = 'JaVaScRiPt:window.__xss=3';

	async function mockInstanceList(page: Page): Promise<void> {
		const json = (body: unknown) => ({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(body)
		});
		await page.route('**/api/v1/auth/setup/status', (r) =>
			r.fulfill(json({ setup_complete: true }))
		);
		await page.route('**/api/v1/auth/session', (r) =>
			r.fulfill(
				json({
					user: { id: '00000000-0000-0000-0000-000000000099', username: 'admin', role: 'admin' },
					csrf_token: 'test-token',
					expires_at: '2099-01-01T00:00:00Z'
				})
			)
		);
		await page.route('**/api/v1/auth/sso', (r) => r.fulfill(json({ oidc: false, proxy: false })));
		await page.route('**/api/v1/instances', (r) =>
			r.fulfill(
				json([
					{
						id: '00000000-0000-0000-0000-000000000001',
						name: hostileName,
						kind: 'sonarr',
						base_url: hostileUrl,
						source: 'manual',
						enabled: true,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				])
			)
		);
	}

	test('encoding: hostile instance fields render as text, never as markup or script', async ({
		page
	}) => {
		const violations = await recordViolations(page);
		await mockInstanceList(page);
		await page.goto('/instances');
		await expect(page.getByRole('link', { name: hostileName })).toBeVisible();
		await expect(page.getByRole('cell', { name: hostileUrl })).toBeVisible();
		expect(
			await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)
		).toBeUndefined();
		await expect(page.locator('main img[src="x"], main script')).toHaveCount(0);
		await expect(page.locator('a[href^="javascript:" i]')).toHaveCount(0);
		expect(await violations()).toEqual([]);
	});
}
