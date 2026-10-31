// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #885

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { expect, test } from '@playwright/test';

const TEST_DAY = '2026-10-19';

test('885: idmatchparity enforces day log coverage and supports file output via -out', () => {
	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'idmatchparity-885-'));
	const snatchLog = path.join(tempDir, 'snatcharr.log');
	const unblockerLog = path.join(tempDir, 'unblocker.log');
	const outFile = path.join(tempDir, 'verdict.txt');

	try {
		// Negative direction: empty logs exit 2 with no-lines-on-day error (go run reports exit status 2).
		fs.writeFileSync(snatchLog, '', 'utf-8');
		fs.writeFileSync(unblockerLog, '2026-10-19T00:00:00Z startup', 'utf-8');

		const emptyRun = spawnSync(
			'go',
			[
				'run',
				'./tools/idmatchparity',
				'-day',
				TEST_DAY,
				'-snatcharr',
				snatchLog,
				'-unblocker',
				unblockerLog
			],
			{ cwd: repoRoot, timeout: 30_000, encoding: 'utf-8' }
		);
		expect(emptyRun.status).toBe(1);
		expect(emptyRun.stderr).toContain('exit status 2');
		expect(emptyRun.stderr).toContain(`no lines on ${TEST_DAY}`);

		// Positive direction: matching day lines exit 0 and write verdict to -out file.
		const snatchContent = JSON.stringify({
			time: `${TEST_DAY}T12:00:00Z`,
			level: 'INFO',
			msg: 'queue: would force import',
			instance: 'sonarr',
			rule: 'test',
			download_id: 'test-dl-1',
			title: 'Item One'
		});
		fs.writeFileSync(snatchLog, `${snatchContent}\n`, 'utf-8');
		fs.writeFileSync(
			unblockerLog,
			`${TEST_DAY}T12:00:00Z [sonarr] IMPORT 'Item One' downloadId=test-dl-1 files=1\n`,
			'utf-8'
		);

		const validRun = spawnSync(
			'go',
			[
				'run',
				'./tools/idmatchparity',
				'-day',
				TEST_DAY,
				'-snatcharr',
				snatchLog,
				'-unblocker',
				unblockerLog,
				'-out',
				outFile
			],
			{ cwd: repoRoot, timeout: 30_000, encoding: 'utf-8' }
		);
		expect(validRun.status).toBe(0);
		expect(validRun.stdout).toContain('verdict=green');
		expect(fs.existsSync(outFile)).toBe(true);
		expect(fs.readFileSync(outFile, 'utf-8')).toContain('verdict=green');
	} finally {
		fs.rmSync(tempDir, { recursive: true, force: true });
	}
});
