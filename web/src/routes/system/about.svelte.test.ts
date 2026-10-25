// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
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
			licence: 'MIT',
			url: 'https://github.com/tokio-rs/tokio',
			text: 'MIT License for Tokio'
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

describe('about page', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders heading, system status fields, licence link and third-party groups', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(sampleLicences);
		const { container } = renderWithQuery(AboutTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.about_app_heading() })
		).toBeTruthy();
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

		expect(
			await screen.findByRole('heading', { level: 3, name: m.about_third_party_group_go() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: m.about_third_party_group_rust() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: m.about_third_party_group_npm() })
		).toBeTruthy();

		expect(screen.getByText('github.com/gin-gonic/gin')).toBeTruthy();
		expect(screen.getByText('tokio')).toBeTruthy();
		expect(screen.getByText('svelte')).toBeTruthy();

		const visitLink = screen.getByRole('link', {
			name: m.about_third_party_visit_aria({ name: 'tokio' })
		});
		expect(visitLink.getAttribute('href')).toBe('https://github.com/tokio-rs/tokio');
		expect(visitLink.getAttribute('target')).toBe('_blank');
		expect(visitLink.getAttribute('rel')).toBe('noopener noreferrer external');

		expect(screen.getAllByText(m.about_third_party_toggle_text()).length).toBe(3);

		const axeResults = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(axeResults.violations).toEqual([]);
	});

	it('negative: fetch error shows message, status stays intact', async () => {
		stubGet({ '/system/status': sampleStatus });
		stubLicences(new Error('failed'));
		renderWithQuery(AboutTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.about_app_heading() })
		).toBeTruthy();
		expect(screen.getByText('SnatchArr')).toBeTruthy();
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
