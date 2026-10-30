// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #926

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, test } from '@playwright/test';

function prepareRoot(tempDir: string, repoRoot: string): { srcDir: string; buildDir: string } {
	const srcTestData = path.join(repoRoot, 'tools/ci/testdata/web_css_classes');
	const dstTestData = path.join(tempDir, 'tools/ci/testdata/web_css_classes');
	fs.cpSync(srcTestData, dstTestData, { recursive: true });

	const srcDir = path.join(tempDir, 'web/src');
	const buildDir = path.join(tempDir, 'web/build');
	fs.mkdirSync(srcDir, { recursive: true });
	fs.mkdirSync(buildDir, { recursive: true });
	return { srcDir, buildDir };
}

test('926: web css classes check catches dead utility classes inside Svelte expressions', () => {
	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const checkScript = path.join(repoRoot, 'tools/ci/web_css_classes_check.py');
	const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'web-css-check-926-'));

	try {
		const { srcDir, buildDir } = prepareRoot(tempDir, repoRoot);
		const cssFile = path.join(buildDir, 'bundle.css');
		const svelteFile = path.join(srcDir, 'App.svelte');

		fs.writeFileSync(
			cssFile,
			'.bg-surface { background: #111; }\n.text-sm { font-size: 14px; }\n',
			'utf-8'
		);

		// Negative direction: dead class in expression ternary is caught with exit 1.
		fs.writeFileSync(
			svelteFile,
			"<div class=\"bg-surface {active ? 'bg-dead-ternary' : 'text-sm'}\"></div>\n",
			'utf-8'
		);
		const plantedRun = spawnSync('python3', [checkScript, '--root', tempDir], {
			timeout: 15_000,
			encoding: 'utf-8'
		});
		expect(plantedRun.status).toBe(1);
		expect(plantedRun.stderr).toContain("missing utility class 'bg-dead-ternary' in built CSS");

		// Positive direction: valid class in expression ternary passes with exit 0.
		fs.writeFileSync(
			svelteFile,
			"<div class=\"bg-surface {active ? 'bg-surface' : 'text-sm'}\"></div>\n",
			'utf-8'
		);
		const cleanRun = spawnSync('python3', [checkScript, '--root', tempDir], {
			timeout: 15_000,
			encoding: 'utf-8'
		});
		expect(cleanRun.status).toBe(0);
		expect(cleanRun.stdout).toContain('0 findings');
	} finally {
		fs.rmSync(tempDir, { recursive: true, force: true });
	}
});
