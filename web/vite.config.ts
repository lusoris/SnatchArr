// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
// Imported from paraglide directly: sveltesentio ships TypeScript sources, which Node cannot
// load while evaluating this config (only Vite transforms them).
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import tailwindcss from '@tailwindcss/vite';
import { svelteTesting } from '@testing-library/svelte/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [
		tailwindcss(),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['cookie', 'preferredLanguage', 'baseLocale']
		}),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// SPA embedded in the API image (ADR-0005): every unknown path serves index.html and
			// the Go server owns sessions and CSRF, so there is no SvelteKit server runtime.
			adapter: adapter({ fallback: 'index.html', precompress: true, strict: false })
		})
	],
	server: {
		// Same-origin in dev too: Vite proxies the API so cookies and CSRF behave like prod.
		proxy: {
			'/api': { target: process.env.SNATCHARR_API ?? 'http://127.0.0.1:8080', changeOrigin: false }
		}
	},
	test: {
		expect: { requireAssertions: true },
		// sveltesentio packages are TypeScript sources: run them through Vite, never plain Node.
		server: { deps: { inline: [/@sveltesentio\//] } },
		coverage: {
			provider: 'v8',
			reporter: ['text', 'json-summary', 'lcov'],
			include: ['src/lib/**/*.{ts,svelte}'],
			exclude: ['src/lib/paraglide/**', 'src/lib/api/schema.d.ts', 'src/lib/**/*.d.ts']
		},
		projects: [
			{
				extends: './vite.config.ts',
				plugins: [svelteTesting()],
				test: {
					name: 'client',
					environment: 'jsdom',
					include: ['src/**/*.svelte.{test,spec}.{js,ts}']
				}
			},
			{
				extends: './vite.config.ts',
				test: {
					name: 'server',
					environment: 'node',
					include: ['src/**/*.{test,spec}.{js,ts}'],
					exclude: ['src/**/*.svelte.{test,spec}.{js,ts}']
				}
			}
		]
	}
});
