// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { DOCS_ENTRIES, HELP_MAP, type DocsEntry, type HelpEntry } from '#lib/help.js';
import { m } from '#lib/paraglide/messages.js';
import PageHeader from './PageHeader.svelte';

const statusSnippet = createRawSnippet(() => ({
	render: () => '<span data-testid="status-badge">Connected</span>'
}));

const actionsSnippet = createRawSnippet(() => ({
	render: () => '<button type="button" data-testid="action-btn">Refresh</button>'
}));

const repoRoot = path.resolve(import.meta.dirname, '../../../../');
const enMessages: Record<string, string> = JSON.parse(
	fs.readFileSync(path.resolve(repoRoot, 'web/messages/en.json'), 'utf-8')
);
const deMessages: Record<string, string> = JSON.parse(
	fs.readFileSync(path.resolve(repoRoot, 'web/messages/de.json'), 'utf-8')
);

function extractHeadingSlugs(markdown: string): string[] {
	const headingRegex = /^#{1,6}\s+(.+)$/gm;
	const slugs: string[] = [];
	let match: RegExpExecArray | null;
	while ((match = headingRegex.exec(markdown)) !== null) {
		const rawTitle = match[1].trim();
		const slug = rawTitle
			.toLowerCase()
			.replace(/[^\w\s-]/g, '')
			.trim()
			.replace(/[\s_]+/g, '-');
		slugs.push(slug);
	}
	return slugs;
}

function validateHelpEntry(
	entry: Pick<HelpEntry, 'summaryKey' | 'docsPath'>,
	enJson: Record<string, string>,
	deJson: Record<string, string>,
	root: string
): void {
	if (!enJson[entry.summaryKey]) {
		throw new Error(`en.json missing summaryKey: ${entry.summaryKey}`);
	}
	if (!deJson[entry.summaryKey]) {
		throw new Error(`de.json missing summaryKey: ${entry.summaryKey}`);
	}
	const [filePath, anchor] = entry.docsPath.split('#');
	const absolutePath = path.resolve(root, 'docs', filePath);
	if (!fs.existsSync(absolutePath)) {
		throw new Error(`docs file does not exist: ${absolutePath}`);
	}
	if (anchor) {
		const content = fs.readFileSync(absolutePath, 'utf-8');
		const slugs = extractHeadingSlugs(content);
		if (!slugs.includes(anchor)) {
			throw new Error(`heading anchor #${anchor} not found in ${filePath}`);
		}
	}
}

function validateDocsEntry(entry: DocsEntry, root: string): void {
	const title = entry.title();
	if (!title || title.trim().length === 0) {
		throw new Error('entry has empty title');
	}
	const [filePath, anchor] = entry.docsPath.split('#');
	const absolutePath = path.resolve(root, 'docs', filePath);
	if (!fs.existsSync(absolutePath)) {
		throw new Error(`docs file does not exist: ${absolutePath}`);
	}
	if (anchor) {
		const content = fs.readFileSync(absolutePath, 'utf-8');
		const slugs = extractHeadingSlugs(content);
		if (!slugs.includes(anchor)) {
			throw new Error(`heading anchor #${anchor} not found in ${filePath}`);
		}
	}
}

describe('PageHeader', () => {
	it('positive: renders title, description, status and actions snippets', async () => {
		const { container } = render(PageHeader, {
			props: {
				title: 'Instances Overview',
				description: 'Manage connected *arr instances',
				status: statusSnippet,
				actions: actionsSnippet
			}
		});

		expect(screen.getByRole('heading', { level: 1, name: 'Instances Overview' })).toBeTruthy();
		expect(screen.getByText('Manage connected *arr instances')).toBeTruthy();
		expect(screen.getByTestId('status-badge').textContent).toBe('Connected');
		expect(screen.getByTestId('action-btn').textContent).toBe('Refresh');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: omits optional description, status and actions when not provided', () => {
		const { container } = render(PageHeader, {
			props: {
				title: 'Minimal Page'
			}
		});

		expect(screen.getByRole('heading', { level: 1, name: 'Minimal Page' })).toBeTruthy();
		expect(container.querySelector('p')).toBeNull();
		expect(container.querySelector('[data-testid="status-badge"]')).toBeNull();
		expect(container.querySelector('[data-testid="action-btn"]')).toBeNull();
	});

	it('boundary: handles empty string title and special characters safely', () => {
		const specialTitle = '<Test & "Special" \'Chars\'>';
		render(PageHeader, {
			props: {
				title: specialTitle
			}
		});

		const heading = screen.getByRole('heading', { level: 1 });
		expect(heading.textContent).toBe(specialTitle);
	});

	it('positive: updates document title when documentTitle is true', () => {
		document.title = 'Previous Title';
		render(PageHeader, {
			props: {
				title: 'Account Settings',
				documentTitle: true
			}
		});

		expect(document.title).toBe(`Account Settings · ${m.app_name()}`);
	});

	it('negative: does not update document title when documentTitle is false or omitted', () => {
		document.title = 'Unchanged Title';
		render(PageHeader, {
			props: {
				title: 'Account Settings'
			}
		});

		expect(document.title).toBe('Unchanged Title');
	});

	it('positive: renders sticky class and children snippet when provided', () => {
		const childrenSnippet = createRawSnippet(() => ({
			render: () => '<nav data-testid="test-nav">Subtabs</nav>'
		}));
		const { container } = render(PageHeader, {
			props: {
				title: 'Sticky Page',
				sticky: true,
				children: childrenSnippet
			}
		});

		const header = container.querySelector('header');
		expect(header?.className).toContain('sticky');
		expect(header?.className).toContain('top-0');
		expect(screen.getByTestId('test-nav').textContent).toBe('Subtabs');
	});

	it('positive: renders 44px help button and opens HelpSheet dialog on click', async () => {
		render(PageHeader, {
			props: {
				title: 'Helpful Page',
				help: {
					summaryKey: 'help_overview',
					docsPath: 'concepts.md'
				}
			}
		});

		const helpBtn = screen.getByRole('button', { name: m.help_button_aria() });
		expect(helpBtn).toBeTruthy();
		expect(helpBtn.className).toContain('h-11');
		expect(helpBtn.className).toContain('w-11');

		expect(screen.queryByRole('dialog')).toBeNull();

		await fireEvent.click(helpBtn);

		const dialog = screen.getByRole('dialog');
		expect(dialog).toBeTruthy();
	});

	it('negative: does not render help button when help prop is omitted', () => {
		render(PageHeader, {
			props: {
				title: 'Page Without Help'
			}
		});

		expect(screen.queryByRole('button', { name: m.help_button_aria() })).toBeNull();
	});

	describe('HELP_MAP contract tests', () => {
		it('positive: every route in HELP_MAP has valid messages and existing docs path/anchor', () => {
			const routes = Object.keys(HELP_MAP);
			expect(routes.length).toBeGreaterThanOrEqual(20);

			for (const route of routes) {
				const entry = HELP_MAP[route];
				expect(() => validateHelpEntry(entry, enMessages, deMessages, repoRoot)).not.toThrow();
			}
		});

		it('negative: planted unknown docs file fails validation', () => {
			const planted = {
				summaryKey: 'help_overview',
				docsPath: 'nonexistent-page-planted.md'
			};
			expect(() => validateHelpEntry(planted, enMessages, deMessages, repoRoot)).toThrow(
				/docs file does not exist/
			);
		});

		it('negative: planted unknown heading anchor fails validation', () => {
			const planted = {
				summaryKey: 'help_overview',
				docsPath: 'concepts.md#nonexistent-anchor-planted'
			};
			expect(() => validateHelpEntry(planted, enMessages, deMessages, repoRoot)).toThrow(
				/heading anchor #nonexistent-anchor-planted not found/
			);
		});

		it('boundary: path without anchor passes without anchor validation', () => {
			const boundary = {
				summaryKey: 'help_system_modules',
				docsPath: 'modules.md'
			};
			expect(() => validateHelpEntry(boundary, enMessages, deMessages, repoRoot)).not.toThrow();
		});
	});

	describe('DOCS_ENTRIES contract tests', () => {
		it('positive: every entry in DOCS_ENTRIES has valid title and existing docs path/anchor', () => {
			expect(DOCS_ENTRIES.length).toBeGreaterThanOrEqual(19);

			for (const entry of DOCS_ENTRIES) {
				expect(() => validateDocsEntry(entry, repoRoot)).not.toThrow();
			}
		});

		it('negative: planted unknown docs entry file fails validation', () => {
			const planted: DocsEntry = {
				title: () => 'Planted Title',
				docsPath: 'nonexistent-page-planted.md'
			};
			expect(() => validateDocsEntry(planted, repoRoot)).toThrow(/docs file does not exist/);
		});

		it('negative: planted unknown heading anchor in docs entry fails validation', () => {
			const planted: DocsEntry = {
				title: () => 'Planted Title',
				docsPath: 'concepts.md#nonexistent-anchor-planted'
			};
			expect(() => validateDocsEntry(planted, repoRoot)).toThrow(
				/heading anchor #nonexistent-anchor-planted not found/
			);
		});

		it('negative: planted empty title fails validation', () => {
			const planted: DocsEntry = {
				title: () => '',
				docsPath: 'concepts.md'
			};
			expect(() => validateDocsEntry(planted, repoRoot)).toThrow(/entry has empty title/);
		});

		it('boundary: entry path without anchor passes without anchor validation', () => {
			const boundary: DocsEntry = {
				title: () => 'Overview',
				docsPath: 'concepts.md'
			};
			expect(() => validateDocsEntry(boundary, repoRoot)).not.toThrow();
		});
	});
});
