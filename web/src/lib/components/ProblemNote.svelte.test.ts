// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import ProblemNote from './ProblemNote.svelte';

describe('ProblemNote', () => {
	it('announces the problem detail as an alert', async () => {
		const { container } = render(ProblemNote, {
			error: new ProblemError({ type: 'about:blank', status: 502, detail: 'Seerr unreachable' })
		});
		expect(screen.getByRole('alert').textContent).toContain('Seerr unreachable');
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('renders nothing without an error', () => {
		const { container } = render(ProblemNote, { error: null });
		expect(container.querySelector('[role="alert"]')).toBeNull();
	});
});
