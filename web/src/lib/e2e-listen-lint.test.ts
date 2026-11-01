// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import path from 'node:path';
import 'tsx/esm';
import { ESLint } from 'eslint';
import { beforeAll, describe, expect, it } from 'vitest';

const webDir = path.resolve(import.meta.dirname, '../..');
const eslint = new ESLint({ cwd: webDir });
const plantedE2eFile = 'e2e/features/planted.test.ts';
const helperFile = 'e2e/features/feature-helpers.ts';

// Warm up the ESLint instance to avoid test timeout during project service initialization.
beforeAll(async () => {
	await eslint.lintText('export {};\n', { filePath: 'src/routes/+layout.ts' });
}, 120_000);

describe('e2e listen lint defence', () => {
	// Verify that bare server.listen calls in e2e tests trigger no-restricted-syntax.
	it('rejects bare server.listen in e2e test files', async () => {
		const code = "server.listen(0, '127.0.0.1', () => {});\n";
		const [result] = await eslint.lintText(code, { filePath: plantedE2eFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	// Verify that feature-helpers.ts is exempted from no-restricted-syntax on server.listen.
	it('permits server.listen inside feature-helpers.ts', async () => {
		const code = "server.listen(0, '127.0.0.1', () => {});\n";
		const [result] = await eslint.lintText(code, { filePath: helperFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).not.toContain('no-restricted-syntax');
	});

	// Verify that listenStub calls in e2e test files do not trigger no-restricted-syntax.
	it('permits await listenStub(server) call in e2e test files', async () => {
		const code = 'await listenStub(server);\n';
		const [result] = await eslint.lintText(code, { filePath: plantedE2eFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).not.toContain('no-restricted-syntax');
	});
});
