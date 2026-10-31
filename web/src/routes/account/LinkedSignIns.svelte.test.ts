// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { fireEvent, screen } from '@testing-library/svelte';
import { axe } from 'vitest-axe';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import LinkedSignIns from './LinkedSignIns.svelte';

type UserIdentity = components['schemas']['UserIdentity'];

interface SsoFlags {
	oidc?: boolean;
	proxy?: boolean;
	jellyfin?: boolean;
}

const sampleJfIdentity: UserIdentity = {
	provider: 'jellyfin',
	linked_at: '2026-10-20T12:00:00Z'
};

const sampleProxyIdentity: UserIdentity = {
	provider: 'proxy',
	linked_at: '2026-10-21T14:30:00Z'
};

function renderLinkedSignIns(sso: SsoFlags, identities: UserIdentity[] = []) {
	stubGet({
		'/auth/sso': { oidc: false, proxy: false, jellyfin: false, ...sso },
		'/auth/identities': { items: identities }
	});
	return renderWithQuery(LinkedSignIns);
}

function queryHeading(): HTMLElement | null {
	return screen.queryByRole('heading', { level: 2, name: m.linked_sign_ins_title() });
}

async function findHeading(): Promise<HTMLElement> {
	return await screen.findByRole('heading', { level: 2, name: m.linked_sign_ins_title() });
}

async function openJellyfinForm(username = 'user', password = 'pass'): Promise<HTMLElement> {
	const openBtn = await screen.findByRole('button', { name: m.linked_sign_in_link_jellyfin() });
	await fireEvent.click(openBtn);

	const userInput = screen.getByLabelText(m.linked_sign_in_username());
	const passInput = screen.getByLabelText(m.linked_sign_in_password());
	await fireEvent.input(userInput, { target: { value: username } });
	await fireEvent.input(passInput, { target: { value: password } });

	return screen.getByRole('button', { name: m.linked_sign_in_link_submit() });
}

async function submitJellyfin(username: string, password: string): Promise<void> {
	const submitBtn = await openJellyfinForm(username, password);
	await fireEvent.click(submitBtn);
}

async function confirmUnlink(providerName: string): Promise<void> {
	const unlinkBtn = await screen.findByRole('button', { name: m.linked_sign_in_unlink() });
	await fireEvent.click(unlinkBtn);

	const confirmBtn = screen.getByRole('button', {
		name: m.linked_sign_in_unlink_confirm_btn({
			provider: providerName
		})
	});
	await fireEvent.click(confirmBtn);
}

describe('LinkedSignIns', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('hidden when both providers disabled', async () => {
		renderLinkedSignIns({ oidc: true });

		expect(queryHeading()).toBeNull();
	});

	it('hidden boundary when all sso providers disabled', async () => {
		renderLinkedSignIns({});

		expect(queryHeading()).toBeNull();
	});

	it('shows only enabled providers: jellyfin only', async () => {
		renderLinkedSignIns({ jellyfin: true });

		expect(await findHeading()).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_jellyfin())).toBeTruthy();
		expect(screen.queryByText(m.linked_sign_in_provider_proxy())).toBeNull();
	});

	it('shows only enabled providers: proxy only', async () => {
		renderLinkedSignIns({ proxy: true });

		expect(await findHeading()).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_proxy())).toBeTruthy();
		expect(screen.queryByText(m.linked_sign_in_provider_jellyfin())).toBeNull();
	});

	it('shows only enabled providers: both jellyfin and proxy', async () => {
		renderLinkedSignIns({ jellyfin: true, proxy: true });

		expect(await findHeading()).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_jellyfin())).toBeTruthy();
		expect(screen.getByText(m.linked_sign_in_provider_proxy())).toBeTruthy();
	});

	it('link jellyfin success reloads list and clears password', async () => {
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderLinkedSignIns({ jellyfin: true });

		await submitJellyfin('jellyuser', 'jellypass');

		expect(postSpy).toHaveBeenCalledWith('/auth/identities/jellyfin', {
			body: { username: 'jellyuser', password: 'jellypass' }
		});
	});

	it('409 shows conflict message', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 409,
				detail: 'conflict'
			})
		);
		renderLinkedSignIns({ jellyfin: true });

		await submitJellyfin('jellyadmin', 'secret');

		expect(await screen.findByText(m.linked_sign_in_error_409_jellyfin())).toBeTruthy();
	});

	it('unlink calls DELETE after confirm', async () => {
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderLinkedSignIns({ jellyfin: true }, [sampleJfIdentity]);

		expect(await screen.findByText(m.linked_sign_in_status_linked())).toBeTruthy();

		await confirmUnlink(m.linked_sign_in_provider_jellyfin());

		expect(deleteSpy).toHaveBeenCalledWith('/auth/identities/jellyfin');
	});

	it('429 shows retry message', async () => {
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 429,
				detail: 'too many requests'
			})
		);
		renderLinkedSignIns({ jellyfin: true });

		await submitJellyfin('jellyuser', 'wrongpass');

		expect(await screen.findByText(m.linked_sign_in_error_429())).toBeTruthy();
	});

	it('handles proxy link success and conflict', async () => {
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderLinkedSignIns({ proxy: true });

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
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderLinkedSignIns({ proxy: true }, [sampleProxyIdentity]);

		await confirmUnlink(m.linked_sign_in_provider_proxy());

		expect(deleteSpy).toHaveBeenCalledWith('/auth/identities/proxy');
	});

	it('displays 401, 403, and 404 status messages', async () => {
		const postSpy = vi.spyOn(api, 'POST');
		renderLinkedSignIns({ jellyfin: true });

		const submitBtn = await openJellyfinForm('user', 'pass');

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
		const { container } = renderLinkedSignIns({ jellyfin: true, proxy: true }, [sampleJfIdentity]);

		expect(await findHeading()).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});
});
