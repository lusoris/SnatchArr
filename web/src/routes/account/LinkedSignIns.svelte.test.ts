// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import LinkedSignIns from './LinkedSignIns.svelte';

type UserIdentity = components['schemas']['UserIdentity'];

const sampleJfIdentity: UserIdentity = {
	provider: 'jellyfin',
	linked_at: '2026-10-20T12:00:00Z'
};

const sampleProxyIdentity: UserIdentity = {
	provider: 'proxy',
	linked_at: '2026-10-21T14:30:00Z'
};

describe('LinkedSignIns', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('hidden when both providers disabled', async () => {
		stubGet({
			'/auth/sso': { oidc: true, proxy: false, jellyfin: false },
			'/auth/identities': { items: [] }
		});
		renderWithQuery(LinkedSignIns);

		expect(screen.queryByRole('heading', { level: 2, name: m.linked_sign_ins_title() })).toBeNull();
	});

	it('hidden boundary when all sso providers disabled', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: false },
			'/auth/identities': { items: [] }
		});
		renderWithQuery(LinkedSignIns);

		expect(screen.queryByRole('heading', { level: 2, name: m.linked_sign_ins_title() })).toBeNull();
	});

	it('shows only enabled providers: jellyfin only', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		renderWithQuery(LinkedSignIns);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.linked_sign_ins_title() })
		).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_jellyfin())).toBeTruthy();
		expect(screen.queryByText(m.linked_sign_in_provider_proxy())).toBeNull();
	});

	it('shows only enabled providers: proxy only', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true, jellyfin: false },
			'/auth/identities': { items: [] }
		});
		renderWithQuery(LinkedSignIns);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.linked_sign_ins_title() })
		).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_proxy())).toBeTruthy();
		expect(screen.queryByText(m.linked_sign_in_provider_jellyfin())).toBeNull();
	});

	it('shows only enabled providers: both jellyfin and proxy', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		renderWithQuery(LinkedSignIns);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.linked_sign_ins_title() })
		).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_jellyfin())).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_proxy())).toBeTruthy();
	});

	it('link jellyfin success reloads list and clears password', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(LinkedSignIns);

		const openBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_jellyfin() });
		await fireEvent.click(openBtn);

		const userInput = screen.getByLabelText(m.linked_sign_in_username());
		const passInput = screen.getByLabelText(m.linked_sign_in_password());
		await fireEvent.input(userInput, { target: { value: 'jellyuser' } });
		await fireEvent.input(passInput, { target: { value: 'jellypass' } });

		const submitBtn = screen.getByRole('button', { name: m.linked_sign_in_link_submit() });
		await fireEvent.click(submitBtn);

		expect(postSpy).toHaveBeenCalledWith('/auth/identities/jellyfin', {
			body: { username: 'jellyuser', password: 'jellypass' }
		});
	});

	it('409 shows conflict message', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 409,
				detail: 'conflict'
			})
		);
		renderWithQuery(LinkedSignIns);

		const openBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_jellyfin() });
		await fireEvent.click(openBtn);

		const userInput = screen.getByLabelText(m.linked_sign_in_username());
		const passInput = screen.getByLabelText(m.linked_sign_in_password());
		await fireEvent.input(userInput, { target: { value: 'jellyadmin' } });
		await fireEvent.input(passInput, { target: { value: 'secret' } });

		const submitBtn = screen.getByRole('button', { name: m.linked_sign_in_link_submit() });
		await fireEvent.click(submitBtn);

		expect(await screen.findByText(m.linked_sign_in_error_409_jellyfin())).toBeTruthy();
	});

	it('unlink calls DELETE after confirm', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [sampleJfIdentity] }
		});
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(LinkedSignIns);

		expect(await screen.findByText(m.linked_sign_in_status_linked())).toBeTruthy();

		const unlinkBtn = screen.getByRole('button', { name: m.linked_sign_in_unlink() });
		await fireEvent.click(unlinkBtn);

		const confirmBtn = screen.getByRole('button', {
			name: m.linked_sign_in_unlink_confirm_btn({
				provider: m.linked_sign_in_provider_jellyfin()
			})
		});
		await fireEvent.click(confirmBtn);

		expect(deleteSpy).toHaveBeenCalledWith('/auth/identities/jellyfin');
	});

	it('429 shows retry message', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 429,
				detail: 'too many requests'
			})
		);
		renderWithQuery(LinkedSignIns);

		const openBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_jellyfin() });
		await fireEvent.click(openBtn);

		const userInput = screen.getByLabelText(m.linked_sign_in_username());
		const passInput = screen.getByLabelText(m.linked_sign_in_password());
		await fireEvent.input(userInput, { target: { value: 'jellyuser' } });
		await fireEvent.input(passInput, { target: { value: 'wrongpass' } });

		const submitBtn = screen.getByRole('button', { name: m.linked_sign_in_link_submit() });
		await fireEvent.click(submitBtn);

		expect(await screen.findByText(m.linked_sign_in_error_429())).toBeTruthy();
	});

	it('handles proxy link success and conflict', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true, jellyfin: false },
			'/auth/identities': { items: [] }
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(LinkedSignIns);

		const linkBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_proxy() });
		await fireEvent.click(linkBtn);

		expect(postSpy).toHaveBeenCalledWith('/auth/identities/proxy');

		postSpy.mockRejectedValueOnce(
			new ProblemError({
				type: 'about:blank',
				status: 409,
				detail: 'proxy conflict'
			})
		);
		await fireEvent.click(linkBtn);
		expect(await screen.findByText(m.linked_sign_in_error_409_proxy())).toBeTruthy();
	});

	it('handles unlink for proxy provider', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true, jellyfin: false },
			'/auth/identities': { items: [sampleProxyIdentity] }
		});
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(LinkedSignIns);

		const unlinkBtn = await screen.findByRole('button', { name: m.linked_sign_in_unlink() });
		await fireEvent.click(unlinkBtn);

		const confirmBtn = screen.getByRole('button', {
			name: m.linked_sign_in_unlink_confirm_btn({
				provider: m.linked_sign_in_provider_proxy()
			})
		});
		await fireEvent.click(confirmBtn);

		expect(deleteSpy).toHaveBeenCalledWith('/auth/identities/proxy');
	});

	it('displays 401, 403, and 404 status messages', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: false, jellyfin: true },
			'/auth/identities': { items: [] }
		});
		const postSpy = vi.spyOn(api, 'POST');
		renderWithQuery(LinkedSignIns);

		const openBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_jellyfin() });
		await fireEvent.click(openBtn);

		const userInput = screen.getByLabelText(m.linked_sign_in_username());
		const passInput = screen.getByLabelText(m.linked_sign_in_password());
		await fireEvent.input(userInput, { target: { value: 'user' } });
		await fireEvent.input(passInput, { target: { value: 'pass' } });

		const submitBtn = screen.getByRole('button', { name: m.linked_sign_in_link_submit() });

		// 401
		postSpy.mockRejectedValueOnce(
			new ProblemError({ type: 'about:blank', status: 401, detail: 'unauthorized' })
		);
		await fireEvent.click(submitBtn);
		expect(await screen.findByText(m.linked_sign_in_error_401_jellyfin())).toBeTruthy();

		// 403
		postSpy.mockRejectedValueOnce(
			new ProblemError({ type: 'about:blank', status: 403, detail: 'forbidden' })
		);
		await fireEvent.click(submitBtn);
		expect(await screen.findByText(m.linked_sign_in_error_403_jellyfin())).toBeTruthy();

		// 404
		postSpy.mockRejectedValueOnce(
			new ProblemError({ type: 'about:blank', status: 404, detail: 'not found' })
		);
		await fireEvent.click(submitBtn);
		expect(await screen.findByText(m.linked_sign_in_error_404_jellyfin())).toBeTruthy();
	});

	it('positive: passes WCAG 2.2 AA accessibility scan', async () => {
		stubGet({
			'/auth/sso': { oidc: false, proxy: true, jellyfin: true },
			'/auth/identities': { items: [sampleJfIdentity] }
		});
		const { container } = renderWithQuery(LinkedSignIns);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.linked_sign_ins_title() })
		).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});
});
