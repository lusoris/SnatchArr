// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { createRawSnippet } from 'svelte';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import Field from './Field.svelte';

const input = createRawSnippet(() => ({ render: () => '<input id="name" />' }));

describe('Field', () => {
	it('labels the control and exposes the hint under a stable id', async () => {
		const { container } = render(Field, {
			id: 'name',
			label: 'Name',
			hint: 'Shown on the dashboard',
			children: input
		});
		expect(screen.getByLabelText('Name')).toBeInstanceOf(HTMLInputElement);
		expect(screen.getByText('Shown on the dashboard').id).toBe('name-hint');
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('omits the hint element when there is none', () => {
		const { container } = render(Field, { id: 'name', label: 'Name', children: input });
		expect(container.querySelector('#name-hint')).toBeNull();
	});
});
