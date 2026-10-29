// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import path from 'node:path';
import 'tsx/esm';
import { ESLint } from 'eslint';
import { beforeAll, describe, expect, it } from 'vitest';

const webDir = path.resolve(import.meta.dirname, '../..');
const eslint = new ESLint({ cwd: webDir });
const sampleRouteFile = 'src/routes/+page.svelte';
const sampleTsFile = 'src/routes/+layout.ts';

// The first lint loads the type-aware project service for the whole web tree, which takes
// longer than the per-test timeout under a parallel verify run. Warm it once here so each
// test below measures only its own lint.
beforeAll(async () => {
	await eslint.lintText('export {};\n', { filePath: sampleTsFile });
	await eslint.lintText('<p>warm</p>\n', { filePath: sampleRouteFile });
}, 120_000);

describe('entities lint rules: planted negatives', () => {
	it('rejects a planted internal href literal starting with "/"', async () => {
		const code = `<script lang="ts">
	const label = 'Instances';
</script>

<a href="/instances">{label}</a>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it('rejects a planted internal href mustache literal: href={"/x"}', async () => {
		const code = `<script lang="ts">
	const label = 'Instances';
</script>

<a href={"/instances"}>{label}</a>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it('rejects a planted internal href template literal: href={`/instances/${id}`}', async () => {
		const code = `<script lang="ts">
	const id = 'inst-1';
</script>

<a href={\`/instances/\${id}\`}>Instance</a>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it("rejects a planted goto('/x') literal call in .svelte", async () => {
		const code = `<script lang="ts">
	import { goto } from '$app/navigation';
	await goto('/instances');
</script>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it('rejects a planted goto(`/x`) template literal call in .svelte', async () => {
		const code = `<script lang="ts">
	import { goto } from '$app/navigation';
	const id = 'inst-1';
	await goto(\`/instances/\${id}\`);
</script>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it("rejects a planted goto('/x') literal call in .ts", async () => {
		const code = `import { goto } from '$app/navigation';

export async function navigate() {
	await goto('/instances');
}
`;
		const [result] = await eslint.lintText(code, { filePath: sampleTsFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it('rejects a planted goto(`/x`) template literal call in .ts', async () => {
		const code = `import { goto } from '$app/navigation';

export async function navigate(id: string) {
	await goto(\`/instances/\${id}\`);
}
`;
		const [result] = await eslint.lintText(code, { filePath: sampleTsFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-syntax');
	});

	it('rejects a planted import of resolve from $app/paths', async () => {
		const code = `<script lang="ts">
	import { resolve } from '$app/paths';
	const href = resolve('/instances');
</script>

<a {href}>Instances</a>
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		const ruleIds = (result?.messages ?? []).map((m) => m.ruleId);
		expect(ruleIds).toContain('no-restricted-imports');
	});
});

describe('entities lint rules: allowed forms', () => {
	it('accepts clean EntityLink, pageHref, /api/ hrefs, and #main skip links', async () => {
		const code = `<script lang="ts">
	import EntityLink from '#lib/components/EntityLink.svelte';
	import { pageHref } from '#lib/entities.js';
</script>

<a href="#main">Skip</a>
<a href="https://example.com" rel="external">External</a>
<a href="/api/v1/auth/oidc/login" rel="external" data-sveltekit-reload>SSO</a>
<a href={pageHref('/instances')}>All</a>
<EntityLink type="instance" id="inst-1" />
`;
		const [result] = await eslint.lintText(code, { filePath: sampleRouteFile });
		expect(result?.messages ?? []).toEqual([]);
	});

	it('permits /api/..., #..., and external https://... in goto and href without triggering no-restricted-syntax', async () => {
		const svelteCode = `<script lang="ts">
	import { goto } from '$app/navigation';
	const id = '1';
	void goto('/api/v1/auth');
	void goto(\`/api/v1/export/\${id}\`);
	void goto('https://example.com/external');
	void goto('#main');
</script>

<a href="#main">Skip</a>
<a href="https://example.com" rel="external">External</a>
<a href="/api/v1/auth" rel="external">API</a>
<a href={"/api/v1/auth"} rel="external">API Mustache</a>
<a href={\`/api/v1/\${id}\`} rel="external">API Template</a>
`;
		const [svelteResult] = await eslint.lintText(svelteCode, { filePath: sampleRouteFile });
		const svelteRestricted = (svelteResult?.messages ?? []).filter(
			(m) => m.ruleId === 'no-restricted-syntax'
		);
		expect(svelteRestricted).toEqual([]);

		const tsCode = `import { goto } from '$app/navigation';

export async function navigate(id: string) {
	await goto('/api/v1/auth');
	await goto(\`/api/v1/export/\${id}\`);
	await goto('https://example.com/external');
	await goto('#main');
}
`;
		const [tsResult] = await eslint.lintText(tsCode, { filePath: sampleTsFile });
		const tsRestricted = (tsResult?.messages ?? []).filter(
			(m) => m.ruleId === 'no-restricted-syntax'
		);
		expect(tsRestricted).toEqual([]);
	});
});
