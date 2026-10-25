// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import type { ServiceStatusSummary } from '#lib/connections.js';
import ConnectionHubCard from './ConnectionHubCard.svelte';

vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}));

const mockSummary: ServiceStatusSummary = {
	status: 'ok',
	counts: { total: 2, healthy: 2, failing: 0, untested: 0 },
	label: 'Operational',
	countLine: '2 linked'
};

describe('ConnectionHubCard', () => {
	it('pending -> renders skeleton placeholder with busy state', () => {
		render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: true,
			error: null,
			summary: mockSummary,
			showAdd: false
		});

		const skeleton = screen.getByTestId('skeleton');
		expect(skeleton).toBeTruthy();
		expect(skeleton.getAttribute('aria-busy')).toBe('true');
		expect(screen.queryByText('Seerr')).toBeNull();
	});

	it('error -> renders ProblemNote and service title link', async () => {
		const { container } = render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: false,
			error: new ProblemError({ type: 'about:blank', status: 503, detail: 'Connection refused' }),
			summary: mockSummary,
			showAdd: false
		});

		expect(screen.queryByTestId('skeleton')).toBeNull();
		const alert = screen.getByRole('alert');
		expect(alert.textContent).toContain('Connection refused');
		const link = screen.getByRole('link', { name: 'Seerr' });
		expect(link.getAttribute('href')).toBe('/connections/seerr');

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('ok -> renders StatusIcon label, count line, description and title link', async () => {
		const { container } = render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: false,
			error: null,
			summary: mockSummary,
			showAdd: false
		});

		expect(screen.queryByTestId('skeleton')).toBeNull();
		expect(screen.queryByRole('alert')).toBeNull();
		expect(screen.getByRole('link', { name: 'Seerr' }).getAttribute('href')).toBe(
			'/connections/seerr'
		);
		expect(screen.getByText('Request management')).toBeTruthy();
		expect(screen.getByText('Operational')).toBeTruthy();
		expect(screen.getByText('2 linked')).toBeTruthy();
		expect(container.querySelector('[data-status="ok"]')).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('showAdd -> renders link to href when true, and omits link when false', () => {
		const { unmount } = render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: false,
			error: null,
			summary: mockSummary,
			showAdd: true
		});

		const addLink = screen.getByRole('link', { name: m.connections_add_link() });
		expect(addLink.getAttribute('href')).toBe('/connections/seerr');

		unmount();

		render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: false,
			error: null,
			summary: mockSummary,
			showAdd: false
		});

		expect(screen.queryByRole('link', { name: m.connections_add_link() })).toBeNull();
	});

	it('total 0 -> renders StatusIcon label once and omits countLine', () => {
		render(ConnectionHubCard, {
			icon: 'film',
			title: 'Seerr',
			href: '/connections/seerr',
			description: 'Request management',
			pending: false,
			error: null,
			summary: {
				status: 'idle',
				counts: { total: 0, healthy: 0, failing: 0, untested: 0 },
				label: 'Not linked',
				countLine: ''
			},
			showAdd: true
		});

		expect(screen.getByText('Not linked')).toBeTruthy();
		expect(screen.queryAllByText('Not linked').length).toBe(1);
	});
});
