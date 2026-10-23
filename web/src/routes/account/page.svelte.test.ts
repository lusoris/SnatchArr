// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AccountPage from './+page.svelte';

// jsdom has no modal dialog API: open and close the way a browser does, firing the close
// event. Focus trapping and Escape run in a real browser in e2e/flow.test.ts.
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
type CreatedApiKey = components['schemas']['CreatedApiKey'];

const sampleApiKey: ApiKey = {
	id: '018f2651-3472-749e-8c38-5188bf287c90',
	name: 'Backup worker',
	scopes: [],
	created_at: '2026-10-16T10:00:00Z',
	last_used_at: '2026-10-16T11:00:00Z',
	expires_at: '2027-10-16T10:00:00Z'
};

const sampleCreatedKey: CreatedApiKey = {
	...sampleApiKey,
	secret: 'shown-once-fixture'
};

const baseStubs = {
	'/auth/api-keys': [sampleApiKey]
};

describe('account', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading and lists API keys from API', async () => {
		stubGet(baseStubs);
		renderWithQuery(AccountPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.account_title() })).toBeTruthy();
		expect(await screen.findByText('Backup worker')).toBeTruthy();
		expect(screen.getByRole('button', { name: m.api_key_create() })).toBeTruthy();
	});

	it('creates API key and shows secret once in dialog', async () => {
		stubGet(baseStubs);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: sampleCreatedKey
		} as never);
		renderWithQuery(AccountPage);

		const input = await screen.findByLabelText(m.api_key_name());
		await fireEvent.input(input, { target: { value: 'New Test Key' } });

		const submitBtn = screen.getByRole('button', { name: m.api_key_create() });
		await fireEvent.click(submitBtn);

		expect(postSpy).toHaveBeenCalledWith('/auth/api-keys', {
			body: { name: 'New Test Key' }
		});

		expect(await screen.findByRole('dialog')).toBeTruthy();
		expect(screen.getByDisplayValue(sampleCreatedKey.secret)).toBeTruthy();
		expect(screen.getByText(m.api_key_secret_warning())).toBeTruthy();
	});

	it('drops secret from DOM and query cache after closing dialog', async () => {
		stubGet(baseStubs);
		vi.spyOn(api, 'POST').mockResolvedValue({
			data: sampleCreatedKey
		} as never);
		const testClient = new QueryClient({
			defaultOptions: { queries: { retry: false, gcTime: 0 } }
		});
		renderWithQuery(AccountPage, undefined, testClient);

		const input = await screen.findByLabelText(m.api_key_name());
		await fireEvent.input(input, { target: { value: 'New Test Key' } });
		await fireEvent.click(screen.getByRole('button', { name: m.api_key_create() }));

		expect(await screen.findByRole('dialog')).toBeTruthy();
		expect(screen.getByDisplayValue(sampleCreatedKey.secret)).toBeTruthy();

		const closeBtn = screen.getByRole('button', { name: m.api_key_close_dialog() });
		await fireEvent.click(closeBtn);

		expect(screen.queryByRole('dialog')).toBeNull();
		expect(screen.queryByDisplayValue(sampleCreatedKey.secret)).toBeNull();
		expect(screen.queryByText(sampleCreatedKey.secret)).toBeNull();

		const queryCacheString = JSON.stringify(testClient.getQueryCache().getAll());
		expect(queryCacheString).not.toContain(sampleCreatedKey.secret);
		const mutationCacheString = JSON.stringify(
			testClient
				.getMutationCache()
				.getAll()
				.map((mutation) => mutation.state)
		);
		expect(mutationCacheString).not.toContain(sampleCreatedKey.secret);
	});

	it('copies secret to clipboard when copy button clicked', async () => {
		stubGet(baseStubs);
		vi.spyOn(api, 'POST').mockResolvedValue({
			data: sampleCreatedKey
		} as never);
		const writeTextMock = vi.fn().mockResolvedValue(undefined);
		Object.assign(navigator, {
			clipboard: { writeText: writeTextMock }
		});

		renderWithQuery(AccountPage);

		const input = await screen.findByLabelText(m.api_key_name());
		await fireEvent.input(input, { target: { value: 'Copyable Key' } });
		await fireEvent.click(screen.getByRole('button', { name: m.api_key_create() }));

		expect(await screen.findByRole('dialog')).toBeTruthy();
		const copyBtn = screen.getByRole('button', { name: m.api_key_copy() });
		await fireEvent.click(copyBtn);

		expect(writeTextMock).toHaveBeenCalledWith(sampleCreatedKey.secret);
		expect(await screen.findByText(m.api_key_copied())).toBeTruthy();
	});

	it('supports revoking API key with in-page confirmation and cancel', async () => {
		stubGet(baseStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(AccountPage);

		const revokeBtn = await screen.findByRole('button', { name: m.api_key_revoke() });
		await fireEvent.click(revokeBtn);

		expect(
			screen.getByText(m.api_key_revoke_confirm_prompt({ name: sampleApiKey.name }))
		).toBeTruthy();
		const cancelBtn = screen.getByRole('button', { name: m.api_key_cancel() });
		const confirmBtn = screen.getByRole('button', { name: m.api_key_revoke_confirm_btn() });
		expect(confirmBtn).toBeTruthy();

		await fireEvent.click(cancelBtn);
		expect(deleteSpy).not.toHaveBeenCalled();
		expect(screen.queryByRole('button', { name: m.api_key_revoke_confirm_btn() })).toBeNull();

		const revokeBtnAgain = screen.getByRole('button', { name: m.api_key_revoke() });
		await fireEvent.click(revokeBtnAgain);
		const confirmBtnAgain = screen.getByRole('button', { name: m.api_key_revoke_confirm_btn() });
		await fireEvent.click(confirmBtnAgain);

		expect(deleteSpy).toHaveBeenCalledWith('/auth/api-keys/{id}', {
			params: { path: { id: sampleApiKey.id } }
		});
	});

	async function fillAndSubmitPassword(current: string, next: string, confirm: string) {
		const currentPass = (await screen.findByLabelText(m.current_password())) as HTMLInputElement;
		const newPass = screen.getByLabelText(m.new_password()) as HTMLInputElement;
		const confirmPass = screen.getByLabelText(m.confirm_new_password()) as HTMLInputElement;
		const submitBtn = screen.getByRole('button', { name: m.change_password_submit() });

		await fireEvent.input(currentPass, { target: { value: current } });
		await fireEvent.input(newPass, { target: { value: next } });
		await fireEvent.input(confirmPass, { target: { value: confirm } });
		await fireEvent.click(submitBtn);

		return { currentPass, newPass, confirmPass };
	}

	it('blocks password change client-side when confirmation does not match', async () => {
		stubGet(baseStubs);
		const postSpy = vi.spyOn(api, 'POST');
		renderWithQuery(AccountPage);

		await fillAndSubmitPassword(
			'current-valid-password',
			'new-valid-password-123',
			'different-password-123'
		);

		expect(postSpy).not.toHaveBeenCalled();
		expect(screen.getByText(m.password_mismatch())).toBeTruthy();
	});

	it('displays 422 problem details with field-level errors when password change fails', async () => {
		stubGet(baseStubs);
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'Password does not meet requirements',
				invalidParams: [{ name: 'new_password', reason: 'password too weak' }]
			})
		);
		renderWithQuery(AccountPage);

		await fillAndSubmitPassword(
			'current-valid-password',
			'new-weak-password-123',
			'new-weak-password-123'
		);

		expect(await screen.findByText('password too weak')).toBeTruthy();
	});

	it('displays 204 success message stating other sessions were ended', async () => {
		stubGet(baseStubs);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(AccountPage);

		const { currentPass, newPass, confirmPass } = await fillAndSubmitPassword(
			'current-valid-password',
			'new-valid-password-123',
			'new-valid-password-123'
		);

		expect(await screen.findByText(m.password_changed_sessions_ended())).toBeTruthy();
		expect(currentPass.value).toBe('');
		expect(newPass.value).toBe('');
		expect(confirmPass.value).toBe('');
	});
});
