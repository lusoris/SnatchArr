// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRawSnippet } from 'svelte';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { installDialogPolyfill, stubGet } from '#lib/testing/harness.js';
import * as nav from '#lib/components/shell/nav.js';
import Layout from './+layout.svelte';

beforeAll(() => {
	window.matchMedia ??= vi.fn().mockImplementation((query: string) => ({
		matches: false,
		media: query,
		onchange: null,
		addListener: vi.fn(),
		removeListener: vi.fn(),
		addEventListener: vi.fn(),
		removeEventListener: vi.fn(),
		dispatchEvent: vi.fn()
	}));
	installDialogPolyfill();
});

vi.mock('$app/paths', () => ({
	resolve: (path: string) => (path.startsWith('/') ? path : `/${path}`)
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

const { mockPage } = vi.hoisted(() => ({
	mockPage: { url: new URL('http://localhost/fixture-disabled') }
}));

vi.mock('$app/state', () => ({
	page: mockPage
}));

const protectedContent = createRawSnippet(() => ({
	render: () => '<div data-testid="protected-content">Content</div>'
}));

function signInAdmin() {
	session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	session.state = 'authenticated';
	vi.spyOn(session, 'refresh').mockResolvedValue('authenticated');
	stubGet({
		'/system/modules': [],
		'/instances': [],
		'/hourly-caps': [],
		'/prowlarr/status': [],
		'/system/status': {
			name: 'SnatchArr',
			version: 'dev',
			commit: '',
			build_date: '',
			profile: 'dev',
			setup_complete: true
		}
	});
}

function renderLayout() {
	render(Layout, { props: { children: protectedContent } });
}

describe('Layout route guard', () => {
	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('positive: renders 404 guard view with module details when route is disabled', async () => {
		signInAdmin();

		vi.spyOn(nav, 'isRouteDisabled').mockReturnValue({
			disabled: true,
			moduleId: 'fixture_mod'
		});

		renderLayout();

		const guardView = await screen.findByTestId('not-found-view');
		expect(guardView).toBeTruthy();
		expect(screen.getByText(m.page_not_found_title())).toBeTruthy();
		expect(screen.getByText(m.page_not_found_text())).toBeTruthy();
		expect(screen.getByRole('alert').textContent).toContain(
			m.module_disabled_detail({ module: 'fixture_mod' })
		);
		expect(screen.queryByTestId('protected-content')).toBeNull();

		const backLink = screen.getByRole('link', { name: m.page_not_found_back() });
		expect(backLink.getAttribute('href')).toBe('/');
	});

	it('negative: renders children content when route is not disabled', async () => {
		signInAdmin();

		vi.spyOn(nav, 'isRouteDisabled').mockReturnValue({
			disabled: false
		});

		renderLayout();

		expect(await screen.findByTestId('protected-content')).toBeTruthy();
		expect(screen.queryByTestId('not-found-view')).toBeNull();
	});
});
