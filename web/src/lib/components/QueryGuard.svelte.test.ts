// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, type Instance } from '#lib/testing/harness.js';
import QueryGuard from './QueryGuard.svelte';

const testChild = createRawSnippet<[unknown]>((getData) => {
	const instance = getData() as Instance | undefined;
	return {
		render: () => `<div data-testid="child">${instance?.name ?? ''}</div>`
	};
});

describe('QueryGuard', () => {
	it('positive: renders children when query data is present and passes WCAG', async () => {
		const { container } = render(QueryGuard, {
			props: {
				query: { isPending: false, isError: false, error: null, data: forkInstance },
				children: testChild
			}
		});

		expect(screen.getByTestId('child').textContent).toBe(forkInstance.name);
		expect(screen.queryByText(m.loading())).toBeNull();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: renders ProblemNote when query encounters an error', async () => {
		const error = new ProblemError({
			type: 'about:blank',
			status: 503,
			detail: 'instance unreachable'
		});
		render(QueryGuard, {
			props: {
				query: { isPending: false, isError: true, error, data: undefined },
				children: testChild
			}
		});

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('instance unreachable');
		expect(screen.queryByTestId('child')).toBeNull();
	});

	it('boundary: renders polite loading indicator while query is pending', () => {
		render(QueryGuard, {
			props: {
				query: { isPending: true, isError: false, error: null, data: undefined },
				children: testChild
			}
		});

		const loading = screen.getByText(m.loading());
		expect(loading.getAttribute('aria-live')).toBe('polite');
		expect(screen.queryByTestId('child')).toBeNull();
	});
});
