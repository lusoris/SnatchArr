// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #902

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';

function runLint(webDir: string, eslintBin: string, relPath: string) {
	return spawnSync('node', ['--import=tsx', eslintBin, relPath], {
		cwd: webDir,
		timeout: 80_000,
		encoding: 'utf-8'
	});
}

test('902: eslint refuses hand-written internal hrefs', () => {
	test.setTimeout(180_000);
	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const webDir = path.join(repoRoot, 'web');
	const eslintBin = path.join(webDir, 'node_modules/eslint/bin/eslint.js');
	const tempFile = path.join(webDir, 'src/lib/_temp_lint_902.svelte');
	const relPath = 'src/lib/_temp_lint_902.svelte';

	try {
		fs.writeFileSync(
			tempFile,
			'<script lang="ts">\n</script>\n<a href="/instances">Instances</a>\n'
		);
		const plantedRun = runLint(webDir, eslintBin, relPath);
		expect(plantedRun.status).not.toBe(0);
		expect(plantedRun.stdout + plantedRun.stderr).toContain('no-restricted-syntax');

		fs.writeFileSync(
			tempFile,
			`<script lang="ts">\n\timport { pageHref } from '#lib/entities.js';\n</script>\n<a href={pageHref('/instances')}>Instances</a>\n`
		);
		const cleanRun = runLint(webDir, eslintBin, relPath);
		expect(cleanRun.status).toBe(0);
	} finally {
		if (fs.existsSync(tempFile)) {
			fs.unlinkSync(tempFile);
		}
	}
});
