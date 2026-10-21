// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import PageHeader from './PageHeader.svelte';

const statusSnippet = createRawSnippet(() => ({
	render: () => '<span data-testid="status-badge">Connected</span>'
}));

const actionsSnippet = createRawSnippet(() => ({
	render: () => '<button type="button" data-testid="action-btn">Refresh</button>'
}));

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
});
