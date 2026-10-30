// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #933

import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { expect, test } from '@playwright/test';

interface PackageEntry {
	ecosystem?: string;
	name?: string;
	version?: string;
	url?: string;
}

test('933: served licences.json carries non-empty URLs for Go packages and labels tests pass', async ({
	page
}) => {
	const res = await page.request.get('/licences.json');
	expect(res.ok()).toBeTruthy();

	const payload = (await res.json()) as { packages?: PackageEntry[] };
	const pkgs = payload.packages ?? [];
	expect(pkgs.length).toBeGreaterThan(0);

	const goPkgs = pkgs.filter((p) => p.ecosystem === 'go');
	expect(goPkgs.length).toBeGreaterThan(0);

	for (const pkg of goPkgs) {
		expect(pkg.url).toBeTruthy();
		expect(typeof pkg.url).toBe('string');
		expect(pkg.url?.trim().length).toBeGreaterThan(0);
	}

	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const testScript = path.join(repoRoot, 'tools/licences/labels_test.py');
	const testRun = spawnSync('python3', [testScript], {
		cwd: repoRoot,
		timeout: 30_000,
		encoding: 'utf-8'
	});
	expect(testRun.status).toBe(0);
});
