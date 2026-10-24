// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import ConnectionsHubPage from './+page.svelte';

vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}));

describe('connections hub page', () => {
	it('positive: renders header and links to all four connection destinations', async () => {
		const { container } = render(ConnectionsHubPage);

		expect(
			screen.getByRole('heading', { level: 1, name: new RegExp(m.nav_connections(), 'i') })
		).toBeTruthy();

		expect(
			screen.getByRole('link', { name: new RegExp(m.nav_download_clients(), 'i') })
		).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_seerr(), 'i') })).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_prowlarr(), 'i') })).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_configarr(), 'i') })).toBeTruthy();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
