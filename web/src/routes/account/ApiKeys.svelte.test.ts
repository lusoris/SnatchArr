// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { flushSync } from 'svelte';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import ApiKeys from './ApiKeys.svelte';

beforeAll(() => {
	HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
		this.setAttribute('open', '');
	};
	HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
		this.removeAttribute('open');
		this.dispatchEvent(new Event('close'));
	};
});

type ApiKey = components['schemas']['ApiKey'];

const sampleApiKey: ApiKey = {
	id: '018f2651-3472-749e-8c38-5188bf287c90',
	name: 'Backup worker',
	scopes: [],
	created_at: '2026-10-16T10:00:00Z',
	last_used_at: '2026-10-16T11:00:00Z',
	expires_at: '2027-10-16T10:00:00Z'
};

const baseStubs = {
	'/auth/api-keys': [sampleApiKey]
};

const admin = { id: 'u1', username: 'admin', role: 'admin' } as never;
const requester = { id: 'u2', username: 'requester', role: 'requester' } as never;
const viewer = { id: 'u3', username: 'viewer', role: 'viewer' } as never;

describe('ApiKeys', () => {
	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('positive: requester sees create form, revoke button, and passes WCAG', async () => {
		session.user = requester;
		stubGet(baseStubs);
		const { container } = renderWithQuery(ApiKeys);

		expect(await screen.findByText('Backup worker')).toBeTruthy();
		expect(screen.getByRole('heading', { level: 3, name: m.api_key_create() })).toBeTruthy();
		expect(screen.getByRole('button', { name: m.api_key_create() })).toBeTruthy();
		expect(screen.getByRole('button', { name: m.api_key_revoke() })).toBeTruthy();
		expect(screen.getByText(m.api_key_actions())).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: viewer sees neither create form nor revoke button', async () => {
		session.user = viewer;
		stubGet(baseStubs);
		renderWithQuery(ApiKeys);

		expect(await screen.findByText('Backup worker')).toBeTruthy();
		expect(screen.queryByRole('heading', { level: 3, name: m.api_key_create() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.api_key_create() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.api_key_revoke() })).toBeNull();
		expect(screen.queryByText(m.api_key_actions())).toBeNull();
	});

	it('boundary: follows session role as it changes', async () => {
		session.user = viewer;
		stubGet(baseStubs);
		renderWithQuery(ApiKeys);

		expect(await screen.findByText('Backup worker')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.api_key_create() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.api_key_revoke() })).toBeNull();

		session.user = requester;
		flushSync();
		expect(screen.getByRole('button', { name: m.api_key_create() })).toBeTruthy();
		expect(screen.getByRole('button', { name: m.api_key_revoke() })).toBeTruthy();

		session.user = admin;
		flushSync();
		expect(screen.getByRole('button', { name: m.api_key_create() })).toBeTruthy();
		expect(screen.getByRole('button', { name: m.api_key_revoke() })).toBeTruthy();

		session.user = null;
		flushSync();
		expect(screen.queryByRole('button', { name: m.api_key_create() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.api_key_revoke() })).toBeNull();
	});
});
