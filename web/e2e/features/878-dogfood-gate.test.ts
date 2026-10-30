// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #878

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, test } from '@playwright/test';

const REPO = 'lusoris/SnatchArr';
const TAG = 'v0.2.2';
const TAG_SHA = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
const PARENT_SHA = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';

function setupApiDir(tempDir: string): string {
	const configPath = path.join(tempDir, 'release-please-config.json');
	fs.writeFileSync(
		configPath,
		JSON.stringify({ packages: { '.': { 'extra-files': ['CHANGELOG.md'] } } }),
		'utf-8'
	);
	fs.writeFileSync(
		path.join(tempDir, `repos_${REPO.replace('/', '_')}_commits_${TAG}.json`),
		JSON.stringify({ sha: TAG_SHA, parents: [{ sha: PARENT_SHA }] }),
		'utf-8'
	);
	fs.writeFileSync(
		path.join(tempDir, `repos_${REPO.replace('/', '_')}_compare_${PARENT_SHA}...${TAG_SHA}.json`),
		JSON.stringify({
			files: [{ filename: 'CHANGELOG.md' }, { filename: '.release-please-manifest.json' }]
		}),
		'utf-8'
	);
	return configPath;
}

test('878: dogfood gate refuses release without dogfood pass and accepts valid pass', () => {
	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const gateScript = path.join(repoRoot, 'tools/release/dogfood_gate.py');
	const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'dogfood-gate-878-'));
	const statusFile = path.join(
		tempDir,
		`repos_${REPO.replace('/', '_')}_commits_${PARENT_SHA}_status.json`
	);

	try {
		const configPath = setupApiDir(tempDir);

		// Negative direction: parent commit lacks dogfood success status -> gate refuses with exit 3.
		fs.writeFileSync(statusFile, JSON.stringify({ statuses: [] }), 'utf-8');
		const refused = spawnSync(
			'python3',
			[
				gateScript,
				'--repository',
				REPO,
				'--tag',
				TAG,
				'--api-dir',
				tempDir,
				'--config',
				configPath
			],
			{ timeout: 15_000, encoding: 'utf-8' }
		);
		expect(refused.status).toBe(3);
		expect(refused.stdout + refused.stderr).toContain('dogfood gate: refused');

		// Positive direction: parent commit has dogfood success status -> gate passes with exit 0.
		fs.writeFileSync(
			statusFile,
			JSON.stringify({ statuses: [{ context: 'snatcharr/dogfood', state: 'success' }] }),
			'utf-8'
		);
		const passed = spawnSync(
			'python3',
			[
				gateScript,
				'--repository',
				REPO,
				'--tag',
				TAG,
				'--api-dir',
				tempDir,
				'--config',
				configPath
			],
			{ timeout: 15_000, encoding: 'utf-8' }
		);
		expect(passed.status).toBe(0);
		expect(passed.stdout).toContain('dogfood gate: pass');
	} finally {
		fs.rmSync(tempDir, { recursive: true, force: true });
	}
});
