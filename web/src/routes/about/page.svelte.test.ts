// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import { formatWhen } from '#lib/stamina.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AboutPage from './+page.svelte';

const sampleStatus = {
	name: 'SnatchArr',
	version: '1.2.3',
	commit: 'abcdef1234567890abcdef1234567890abcdef12',
	build_date: '2026-10-23T10:00:00Z',
	profile: 'prod' as const,
	setup_complete: true
};

describe('about page', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders heading, system status fields, licence link and third-party section', async () => {
		stubGet({ '/system/status': sampleStatus });
		const { container } = renderWithQuery(AboutPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.about_title() })).toBeTruthy();
		expect(screen.getByText('SnatchArr')).toBeTruthy();
		expect(screen.getByText('1.2.3')).toBeTruthy();
		expect(screen.getByText('abcdef1234567890abcdef1234567890abcdef12')).toBeTruthy();

		const formattedDate = formatWhen(sampleStatus.build_date, getLocale());
		expect(screen.getByText(formattedDate)).toBeTruthy();

		const licenseLink = screen.getByRole('link', { name: m.about_license_aria() });
		expect(licenseLink.getAttribute('href')).toBe(
			'https://interoperable-europe.ec.europa.eu/collection/eupl/eupl-text-eupl-12'
		);
		expect(licenseLink.getAttribute('target')).toBe('_blank');
		expect(licenseLink.getAttribute('rel')).toBe('noopener noreferrer');

		expect(
			screen.getByRole('heading', { level: 2, name: m.about_third_party_title() })
		).toBeTruthy();
		expect(screen.getByText(m.about_third_party_placeholder())).toBeTruthy();

		const axeResults = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(axeResults.violations).toEqual([]);
	});

	it('negative: a failed status request shows no fields, the licence sections stay', async () => {
		stubGet({ '/system/status': new Error('connection refused') });
		renderWithQuery(AboutPage);

		await waitFor(() => expect(screen.queryByText(m.loading())).toBeNull());
		expect(screen.queryByText(m.about_field_version())).toBeNull();
		expect(
			screen.getByRole('heading', { level: 2, name: m.about_third_party_title() })
		).toBeTruthy();
	});

	it('boundary: handles empty commit and build date gracefully', async () => {
		stubGet({
			'/system/status': {
				name: 'SnatchArr',
				version: 'dev',
				commit: '',
				build_date: '',
				profile: 'dev' as const,
				setup_complete: true
			}
		});
		renderWithQuery(AboutPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.about_title() })).toBeTruthy();
		expect(screen.getByText('dev')).toBeTruthy();
		expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(1);
	});
});
