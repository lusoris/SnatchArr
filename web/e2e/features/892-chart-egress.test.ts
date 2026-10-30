// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// covers: #892

import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';

const EXPECTED_EXCEPT_CIDRS = [
	'10.0.0.0/8',
	'172.16.0.0/12',
	'192.168.0.0/16',
	'100.64.0.0/10',
	'169.254.0.0/16',
	'fc00::/7',
	'fe80::/10'
];

function checkValuesEgress(valuesContent: string) {
	expect(valuesContent).toMatch(/publicHttpsEgress:\s*\n\s*enabled: true/);
	for (const cidr of EXPECTED_EXCEPT_CIDRS) {
		expect(valuesContent).toContain(cidr);
	}
}

function checkHelmOutput(chartDir: string) {
	const templateRun = spawnSync(
		'helm',
		['template', 'snatcharr', chartDir, '--set', 'networkPolicy.enabled=true'],
		{ timeout: 30_000, encoding: 'utf-8' }
	);
	expect(templateRun.status).toBe(0);

	const output = templateRun.stdout;
	expect(output).toContain('name: snatcharr-api');
	expect(output).toContain('port: 443');
	expect(output).toContain('cidr: 0.0.0.0/0');
	expect(output).toContain('cidr: ::/0');

	for (const cidr of EXPECTED_EXCEPT_CIDRS) {
		expect(output).toContain(cidr);
	}

	const workerSection = output.split('name: snatcharr-worker')[1] ?? '';
	const workerNetPol = workerSection.split('---')[0] ?? '';
	expect(workerNetPol).not.toContain('0.0.0.0/0');
}

test('892: helm chart defines public HTTPS egress with private exceptions by default', () => {
	const repoRoot = path.resolve(import.meta.dirname, '../../..');
	const chartDir = path.join(repoRoot, 'deploy/helm/snatcharr');
	const valuesPath = path.join(chartDir, 'values.yaml');

	const valuesContent = fs.readFileSync(valuesPath, 'utf-8');
	checkValuesEgress(valuesContent);

	const helmCheck = spawnSync('which', ['helm'], { encoding: 'utf-8' });
	if (helmCheck.status !== 0) {
		test.info().annotations.push({
			type: 'warning',
			description: 'helm not found on PATH; template output skipped'
		});
		return;
	}

	checkHelmOutput(chartDir);
});
