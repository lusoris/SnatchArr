// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createRawSnippet } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { ProblemError } from '@sveltesentio/core';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import type { LinkItem } from '#lib/components/LinkCard.svelte';
import LinkListSection, { type QueryLike } from './LinkListSection.svelte';

interface TestLink extends LinkItem {
	id: string;
}

const sampleItems: TestLink[] = [
	{ id: 'l1', name: 'Indexer Alpha', base_url: 'http://alpha.local:9696' },
	{ id: 'l2', name: 'Indexer Beta', base_url: 'http://beta.local:9696' }
];

const aside = createRawSnippet(() => ({
	render: () => '<div data-testid="aside-slot">Aside Content</div>'
}));

const extraActions = createRawSnippet<[TestLink]>((getLink) => ({
	render: () => `<button type="button" data-testid="extra">${getLink().id}</button>`
}));

function query(over: Partial<QueryLike<TestLink>>): QueryLike<TestLink> {
	return { isPending: false, isError: false, error: null, data: sampleItems, ...over };
}

function props(q: QueryLike<TestLink>) {
	return {
		idPrefix: 'test',
		title: 'Test Links',
		emptyText: 'No links available',
		query: q,
		editor: { open: vi.fn() },
		probe: { isPending: false, mutate: vi.fn() },
		remove: { isPending: false, mutate: vi.fn() },
		aside
	};
}

describe('LinkListSection', () => {
	afterEach(() => {
		session.user = null;
	});

	it('positive: renders a card per link, the aside and the section label, passes WCAG 2.2 AA', async () => {
		const { container } = render(LinkListSection<TestLink>, { props: props(query({})) });

		expect(screen.getByRole('heading', { level: 3, name: /Indexer Alpha/ })).toBeTruthy();
		expect(screen.getByRole('heading', { level: 3, name: /Indexer Beta/ })).toBeTruthy();
		expect(screen.getByTestId('aside-slot').textContent).toBe('Aside Content');
		expect(screen.getByRole('region', { name: 'Test Links' })).toBeTruthy();

		const a11y = await axe(container, { runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] } });
		expect(a11y.violations).toEqual([]);
	});

	it('positive: renders extra actions once per link for an admin', () => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		render(LinkListSection<TestLink>, { props: { ...props(query({})), extraActions } });

		expect(screen.getAllByTestId('extra').map((b) => b.textContent)).toEqual(['l1', 'l2']);
	});

	it('negative: renders ProblemNote on query error and no cards', async () => {
		const error = new ProblemError({
			type: 'about:blank',
			status: 503,
			detail: 'connection refused'
		});
		render(LinkListSection<TestLink>, {
			props: props(query({ isError: true, error, data: undefined }))
		});

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('connection refused');
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});

	it('boundary: renders emptyText when the list is empty', () => {
		render(LinkListSection<TestLink>, { props: props(query({ data: [] })) });

		expect(screen.getByText('No links available')).toBeTruthy();
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});

	it('boundary: renders a polite loading line while pending', () => {
		render(LinkListSection<TestLink>, {
			props: props(query({ isPending: true, data: undefined }))
		});

		expect(screen.getByText(m.loading()).getAttribute('aria-live')).toBe('polite');
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});
});
