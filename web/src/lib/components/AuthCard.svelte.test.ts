// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { axe } from 'vitest-axe';
import { createRawSnippet } from 'svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import AuthCard from './AuthCard.svelte';

const childrenSnippet = createRawSnippet(() => ({
	render: () => '<form data-testid="auth-form"><button type="submit">Submit</button></form>'
}));

const noticeSnippet = createRawSnippet(() => ({
	render: () => '<p role="alert" data-testid="auth-notice">Notice alert</p>'
}));

describe('AuthCard', () => {
	it('positive: renders heading, intro, children and passes WCAG 2.2 AA', async () => {
		const { container } = render(AuthCard, {
			props: {
				title: 'Sign In',
				intro: 'Welcome back',
				children: childrenSnippet
			}
		});

		const heading = screen.getByRole('heading', { level: 1, name: 'Sign In' });
		expect(heading).toBeTruthy();
		expect(heading.classList.contains('mb-2')).toBe(true);
		expect(screen.getByText('Welcome back')).toBeTruthy();
		expect(screen.getByTestId('auth-form')).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toHaveLength(0);
	});

	it('negative: omits intro p element and uses mb-4 heading margin when intro is absent', () => {
		const { container } = render(AuthCard, {
			props: { title: 'Login Prompt', children: childrenSnippet }
		});

		const h1 = screen.getByRole('heading', { level: 1, name: 'Login Prompt' });
		expect(h1.className).toContain('mb-4');
		expect(container.querySelectorAll('p')).toHaveLength(0);
	});

	it('boundary: renders notice before form and handles empty intro string', () => {
		const { container } = render(AuthCard, {
			props: {
				title: 'Boundary Check',
				intro: '',
				notice: noticeSnippet,
				children: childrenSnippet
			}
		});

		const heading = screen.getByRole('heading', { level: 1, name: 'Boundary Check' });
		expect(heading.classList.contains('mb-4')).toBe(true);
		expect(container.querySelector('p:not([role="alert"])')).toBeNull();

		const notice = screen.getByTestId('auth-notice');
		const form = screen.getByTestId('auth-form');
		expect(Boolean(notice.compareDocumentPosition(form) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(
			true
		);
	});
});
