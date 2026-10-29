import prettier from 'eslint-config-prettier';
import path from 'node:path';
import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import { defineConfig, includeIgnoreFile } from 'eslint/config';
import globals from 'globals';
import ts from 'typescript-eslint';
import sentio from '@sveltesentio/core/eslint';

const gitignorePath = path.resolve(import.meta.dirname, '.gitignore');

export default defineConfig(
	includeIgnoreFile(gitignorePath),
	js.configs.recommended,
	ts.configs.recommended,
	svelte.configs.recommended,
	prettier,
	svelte.configs.prettier,
	{
		languageOptions: { globals: { ...globals.browser, ...globals.node } },
		rules: {
			// typescript-eslint strongly recommend that you do not use the no-undef lint rule on TypeScript projects.
			// see: https://typescript-eslint.io/troubleshooting/faqs/eslint/#i-get-errors-from-the-no-undef-rule-about-global-variables-not-being-defined-even-though-there-are-no-typescript-errors
			'no-undef': 'off'
		}
	},
	{
		files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
		languageOptions: {
			parserOptions: {
				projectService: true,
				extraFileExtensions: ['.svelte'],
				parser: ts.parser
			}
		}
	},
	{
		ignores: [
			'src/lib/paraglide/**',
			'src/lib/api/schema.d.ts',
			'build/**',
			'.svelte-kit/**',
			'coverage/**'
		]
	},
	{
		files: ['src/**/*.ts', 'src/**/*.svelte'],
		plugins: { '@sveltesentio': sentio },
		rules: {
			'@sveltesentio/no-direct-time': 'error',
			'@sveltesentio/chart-a11y-wrapper': 'error',
			'svelte/button-has-type': 'error'
		}
	},
	{
		files: ['src/**/*.ts', 'src/**/*.svelte'],
		ignores: ['src/lib/entities.ts', '**/*.test.ts'],
		rules: {
			'no-restricted-imports': [
				'error',
				{
					paths: [
						{
							name: '$app/paths',
							importNames: ['resolve'],
							message: 'Import pageHref or entityHref from #lib/entities.js instead of resolve.'
						}
					]
				}
			],
			'no-restricted-syntax': [
				'error',
				{
					selector: "SvelteAttribute[key.name='href'] > SvelteLiteral[value=/^\\/(?!api\\/)/]",
					message:
						'Use pageHref, entityHref, or EntityLink from #lib/entities.js for internal hrefs.'
				},
				{
					selector:
						"SvelteAttribute[key.name='href'] > SvelteMustacheTag > Literal[value=/^\\/(?!api\\/)/]",
					message:
						'Use pageHref, entityHref, or EntityLink from #lib/entities.js for internal hrefs.'
				},
				{
					selector:
						"SvelteAttribute[key.name='href'] > SvelteMustacheTag > TemplateLiteral > TemplateElement:first-child[value.raw=/^\\/(?!api\\/)/]",
					message:
						'Use pageHref, entityHref, or EntityLink from #lib/entities.js for internal hrefs.'
				},
				{
					selector:
						"CallExpression[callee.name='goto'] > Literal:first-child[value=/^\\/(?!api\\/)/]",
					message:
						'Use pageHref, entityHref, or EntityLink from #lib/entities.js for internal routes in goto().'
				},
				{
					selector:
						"CallExpression[callee.name='goto'] > TemplateLiteral:first-child > TemplateElement:first-child[value.raw=/^\\/(?!api\\/)/]",
					message:
						'Use pageHref, entityHref, or EntityLink from #lib/entities.js for internal routes in goto().'
				}
			]
		}
	}
);
