// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import type { LicencesData } from '#lib/queries/licences.js';
import { formatWhen } from '#lib/stamina.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AboutTab from './AboutTab.svelte';

const sampleStatus = {
	name: 'SnatchArr',
	version: '1.2.3',
	commit: 'abcdef1234567890abcdef1234567890abcdef12',
	build_date: '2026-10-23T10:00:00Z',
	profile: 'prod' as const,
	setup_complete: true
};

const sampleLicences: LicencesData = {
	generatedBy: {
		'cargo-about': '0.9.2',
		'go-licenses': '2.0.1',
		pnpm: '12.11.2'
	},
	packages: [
		{
			ecosystem: 'go',
			name: 'github.com/gin-gonic/gin',
			version: 'v1.10.0',
			licence: 'MIT',
			url: 'https://github.com/gin-gonic/gin',
			text: 'MIT License for Gin'
		},
		{
			ecosystem: 'rust',
			name: 'tokio',
			version: '1.40.0',
			licence: 'Apache-2.0',
			url: 'https://github.com/tokio-rs/tokio',
			text: 'Apache License for Tokio'
		},
		{
			ecosystem: 'npm',
			name: 'svelte',
			version: '5.0.0',
			licence: 'MIT',
			url: 'https://github.com/sveltejs/svelte',
			text: 'MIT License for Svelte'
		}
	]
};

function stubLicences(dataOrError: LicencesData | Error) {
	return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
		const url = String(input);
		if (url.endsWith('/licences.json')) {
			if (dataOrError instanceof Error) {
				return new Response('Internal Server Error', {
					status: 500,
					statusText: 'Server Error'
				});
			}
			return new Response(JSON.stringify(dataOrError), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			});
		}
		throw new Error(`Unexpected fetch: ${url}`);
	});
}

async function renderAboutTab(): Promise<ReturnType<typeof renderWithQuery>> {
	const rendered = renderWithQuery(AboutTab);
	expect(
		await screen.findByRole('heading', { level: 2, name: m.about_app_heading() })
	).toBeTruthy();
	expect(screen.getByText('SnatchArr')).toBeTruthy();
	return rendered;
}

describe('about page', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders heading, status fields, tabs with counts, scroll region, and passes axe', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(sampleLicences);
		const { container } = await renderAboutTab();

		expect(screen.getByText('1.2.3')).toBeTruthy();
		expect(screen.getByText('abcdef1234567890abcdef1234567890abcdef12')).toBeTruthy();

		const formattedDate = formatWhen(sampleStatus.build_date, getLocale());
		expect(screen.getByText(formattedDate)).toBeTruthy();

		const licenseLink = screen.getByRole('link', { name: m.about_license_aria() });
		expect(licenseLink.getAttribute('href')).toBe(
			'https://interoperable-europe.ec.europa.eu/collection/eupl/eupl-text-eupl-12'
		);

		expect(screen.getByRole('heading', { level: 2, name: m.about_brand_heading() })).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 2, name: m.about_third_party_title() })
		).toBeTruthy();

		const goTabName = m.about_third_party_tab({ name: m.about_third_party_group_go(), count: 1 });
		const rustTabName = m.about_third_party_tab({
			name: m.about_third_party_group_rust(),
			count: 1
		});
		const npmTabName = m.about_third_party_tab({
			name: m.about_third_party_group_npm(),
			count: 1
		});

		const goTab = await screen.findByRole('tab', { name: goTabName });
		const rustTab = screen.getByRole('tab', { name: rustTabName });
		const npmTab = screen.getByRole('tab', { name: npmTabName });

		expect(goTab.getAttribute('aria-selected')).toBe('true');
		expect(rustTab.getAttribute('aria-selected')).toBe('false');
		expect(npmTab.getAttribute('aria-selected')).toBe('false');

		// Default active ecosystem is Go
		expect(screen.getByText('github.com/gin-gonic/gin')).toBeTruthy();
		expect(screen.queryByText('tokio')).toBeNull();
		expect(screen.queryByText('svelte')).toBeNull();

		// Scroll region has role=region, tabindex=0, aria-label
		const region = screen.getByRole('region', {
			name: m.about_third_party_list_label({ name: m.about_third_party_group_go() })
		});
		expect(region.getAttribute('tabindex')).toBe('0');
		expect(region.className).toContain('overflow-y-auto');

		const visitLink = screen.getByRole('link', {
			name: m.about_third_party_visit_aria({ name: 'github.com/gin-gonic/gin' })
		});
		expect(visitLink.getAttribute('href')).toBe('https://github.com/gin-gonic/gin');

		const axeResults = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(axeResults.violations).toEqual([]);
	});

	it('positive: switching tab shows only that ecosystem', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(sampleLicences);
		await renderAboutTab();

		const rustTabName = m.about_third_party_tab({
			name: m.about_third_party_group_rust(),
			count: 1
		});
		const npmTabName = m.about_third_party_tab({
			name: m.about_third_party_group_npm(),
			count: 1
		});

		const rustTab = await screen.findByRole('tab', { name: rustTabName });
		await fireEvent.click(rustTab);

		expect(screen.getByText('tokio')).toBeTruthy();
		expect(screen.queryByText('github.com/gin-gonic/gin')).toBeNull();
		expect(screen.queryByText('svelte')).toBeNull();

		const npmTab = screen.getByRole('tab', { name: npmTabName });
		await fireEvent.click(npmTab);

		expect(screen.getByText('svelte')).toBeTruthy();
		expect(screen.queryByText('github.com/gin-gonic/gin')).toBeNull();
		expect(screen.queryByText('tokio')).toBeNull();
	});

	it('positive: filter by name and filter by licence (case-insensitive)', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(sampleLicences);
		await renderAboutTab();

		const searchInput = await screen.findByRole('searchbox', {
			name: m.about_third_party_filter_label()
		});

		// Filter by name (substring)
		await fireEvent.input(searchInput, { target: { value: 'gin' } });
		expect(screen.getByText('github.com/gin-gonic/gin')).toBeTruthy();

		// Filter by licence (case-insensitive substring)
		await fireEvent.input(searchInput, { target: { value: 'mit' } });
		expect(screen.getByText('github.com/gin-gonic/gin')).toBeTruthy();

		// No match shows empty filter message
		await fireEvent.input(searchInput, { target: { value: 'nonexistent' } });
		expect(screen.getByText(m.about_third_party_no_match())).toBeTruthy();
		expect(screen.queryByText('github.com/gin-gonic/gin')).toBeNull();
	});

	it('boundary: ecosystem with 0 packages has no tab and defaults to first non-empty', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences({
			generatedBy: sampleLicences.generatedBy,
			packages: [
				{
					ecosystem: 'rust',
					name: 'tokio',
					version: '1.40.0',
					licence: 'Apache-2.0',
					url: 'https://github.com/tokio-rs/tokio',
					text: 'Apache License'
				}
			]
		});
		await renderAboutTab();

		// Go and npm have 0 packages: no tabs
		expect(screen.queryByRole('tab', { name: /Go/ })).toBeNull();
		expect(screen.queryByRole('tab', { name: /npm/ })).toBeNull();

		// Rust tab rendered and active by default
		const rustTab = await screen.findByRole('tab', {
			name: m.about_third_party_tab({ name: m.about_third_party_group_rust(), count: 1 })
		});
		expect(rustTab.getAttribute('aria-selected')).toBe('true');
		expect(screen.getByText('tokio')).toBeTruthy();
	});

	it('negative: fetch error shows message, status stays intact', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(new Error('failed'));
		await renderAboutTab();

		expect(await screen.findByRole('alert')).toBeTruthy();
	});

	it('boundary: empty packages list shows empty message', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences({
			generatedBy: sampleLicences.generatedBy,
			packages: []
		});
		renderWithQuery(AboutTab);

		expect(await screen.findByText(m.about_third_party_empty())).toBeTruthy();
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
		stubLicences(sampleLicences);
		renderWithQuery(AboutTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.about_app_heading() })
		).toBeTruthy();
		expect(screen.getByText('dev')).toBeTruthy();
		expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(1);
	});
});
