// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { createRawSnippet, flushSync } from 'svelte';
import { axe } from 'vitest-axe';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { session } from '#lib/session.svelte.js';
import CreateForm from './CreateForm.svelte';

const formContent = createRawSnippet(() => ({
	render: () =>
		'<div><input id="name" aria-label="Name" /><button type="submit">Submit</button></div>'
}));

const boundaryContent = createRawSnippet(() => ({
	render: () => '<span data-testid="empty-content"></span>'
}));

describe('CreateForm', () => {
	afterEach(() => {
		session.user = null;
	});

	it('positive: renders heading, children, handles submission with preventDefault and passes WCAG 2.2 AA', async () => {
		const onsubmit = vi.fn();
		const { container } = render(CreateForm, {
			props: {
				heading: 'Add Item',
				onsubmit,
				children: formContent,
				admin: true
			}
		});

		expect(screen.getByRole('heading', { level: 4, name: 'Add Item' })).toBeTruthy();
		expect(screen.getByRole('textbox', { name: 'Name' })).toBeTruthy();
		const submitBtn = screen.getByRole('button', { name: 'Submit' });

		await fireEvent.click(submitBtn);
		expect(onsubmit).toHaveBeenCalledTimes(1);

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: does not invoke onsubmit without submission and hides form when admin is false', () => {
		const onsubmit = vi.fn();
		const { container } = render(CreateForm, {
			props: {
				heading: 'Unsubmitted Form',
				onsubmit,
				children: formContent,
				admin: false
			}
		});

		expect(container.querySelector('form.card')).toBeNull();
		expect(onsubmit).not.toHaveBeenCalled();
	});

	it('boundary: handles empty heading and minimal children snippet cleanly', () => {
		const onsubmit = vi.fn();
		const { container } = render(CreateForm, {
			props: {
				heading: '',
				onsubmit,
				children: boundaryContent,
				admin: true
			}
		});

		const heading = container.querySelector('h4');
		expect(heading).toBeTruthy();
		expect(heading?.textContent).toBe('');
		expect(screen.getByTestId('empty-content')).toBeTruthy();
		expect(container.querySelector('form.card')).toBeTruthy();
	});

	it('boundary: without the admin prop the form follows the session as it loads', () => {
		session.user = null;
		const { container } = render(CreateForm, {
			props: { heading: 'Add Item', onsubmit: vi.fn(), children: formContent }
		});
		expect(container.querySelector('form.card')).toBeNull();

		session.user = { id: 'u1', username: 'u1', role: 'admin' } as never;
		flushSync();
		expect(container.querySelector('form.card')).toBeTruthy();

		session.user = { id: 'u2', username: 'u2', role: 'viewer' } as never;
		flushSync();
		expect(container.querySelector('form.card')).toBeNull();
	});
});
