// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// pnpm-workspace.yaml forces minimatch 11 (GHSA-g3hv-75wg-m249) and patches
// @redocly/openapi-core, which `pnpm gen:api` loads through openapi-typescript,
// to use minimatch's named export. Without the patch, Redocly's header matching
// throws "minimatch is not a function"; `gen:api` reads a local file and never
// reaches that code, so this test is the guard.

import { createRequire } from 'node:module';
import { describe, expect, it } from 'vitest';

type HeaderRule = { matches: string; name: string; value: string };
type FetchStub = (
	url: string,
	init: { headers: Record<string, string> }
) => Promise<{ ok: true; text: () => Promise<string>; headers: { get: () => string } }>;
type ReadFileFromUrl = (
	url: string,
	config: { headers: HeaderRule[]; customFetch: FetchStub }
) => Promise<{ body: string }>;

const fromOpenapiTypescript = createRequire(
	createRequire(import.meta.url).resolve('openapi-typescript')
);
const { readFileFromUrl } = fromOpenapiTypescript('@redocly/openapi-core') as {
	readFileFromUrl: ReadFileFromUrl;
};

const echoHeaders: FetchStub = async (_url, init) => ({
	ok: true,
	text: async () => JSON.stringify(init.headers),
	headers: { get: () => 'application/json' }
});

async function sentHeaders(url: string, matches: string): Promise<Record<string, string>> {
	const result = await readFileFromUrl(url, {
		headers: [{ matches, name: 'X-Probe', value: 'v' }],
		customFetch: echoHeaders
	});
	return JSON.parse(result.body) as Record<string, string>;
}

describe('Redocly header matching under minimatch 11', () => {
	it('sends a header whose pattern matches the URL', async () => {
		expect(await sentHeaders('https://example.com/a/openapi.yaml', 'example.com/**')).toEqual({
			'X-Probe': 'v'
		});
	});

	it('omits a header whose pattern does not match', async () => {
		expect(await sentHeaders('https://example.com/a/openapi.yaml', 'other.example/**')).toEqual({});
	});

	it('matches a pattern that names the protocol against the full URL', async () => {
		expect(await sentHeaders('https://example.com/openapi.yaml', 'http://example.com/*')).toEqual(
			{}
		);
		expect(await sentHeaders('https://example.com/openapi.yaml', 'https://example.com/*')).toEqual({
			'X-Probe': 'v'
		});
	});
});
