// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { RequestsSettings } from '#lib/requestsSettings.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { assertProblemAlert, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import RequestsSettingsPage from './+page.svelte';

const defaultSettings: RequestsSettings = {
	voting_mode: 'upvotes_only',
	allow_viewer_votes: true,
	visibility: 'public_attributed',
	voting_ttl_days: 0
};

describe('RequestsSettingsPage', () => {
	beforeEach(() => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders heading and form controls with default settings', async () => {
		stubGet({
			'/requests/settings': defaultSettings
		});
		renderWithQuery(RequestsSettingsPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.requests_settings_title() })
		).toBeTruthy();

		const votingMode = (await screen.findByLabelText(
			m.requests_settings_voting_mode()
		)) as HTMLSelectElement;
		const allowViewer = screen.getByLabelText(
			m.requests_settings_allow_viewer_votes()
		) as HTMLInputElement;
		const visibility = screen.getByLabelText(m.requests_settings_visibility()) as HTMLSelectElement;
		const ttl = screen.getByLabelText(m.requests_settings_voting_ttl_days()) as HTMLInputElement;

		expect(votingMode.value).toBe('upvotes_only');
		expect(allowViewer.checked).toBe(true);
		expect(visibility.value).toBe('public_attributed');
		expect(ttl.value).toBe('0');
		expect(screen.getByRole('button', { name: m.instance_save() })).toBeTruthy();
	});

	it('announces problem when requests settings query fails', async () => {
		stubGet({
			'/requests/settings': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'failed to read requests settings'
			})
		});
		renderWithQuery(RequestsSettingsPage);
		await assertProblemAlert('failed to read requests settings');
	});

	it('submits updated settings when save button is clicked by admin', async () => {
		stubGet({
			'/requests/settings': defaultSettings
		});
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: {
				...defaultSettings,
				voting_mode: 'net_score',
				allow_viewer_votes: false,
				visibility: 'private_to_requester_and_admin',
				voting_ttl_days: 14
			}
		} as never);
		renderWithQuery(RequestsSettingsPage);

		const votingMode = (await screen.findByLabelText(
			m.requests_settings_voting_mode()
		)) as HTMLSelectElement;
		const allowViewer = screen.getByLabelText(
			m.requests_settings_allow_viewer_votes()
		) as HTMLInputElement;
		const visibility = screen.getByLabelText(m.requests_settings_visibility()) as HTMLSelectElement;
		const ttl = screen.getByLabelText(m.requests_settings_voting_ttl_days()) as HTMLInputElement;

		await fireEvent.change(votingMode, { target: { value: 'net_score' } });
		await fireEvent.click(allowViewer);
		await fireEvent.change(visibility, { target: { value: 'private_to_requester_and_admin' } });
		await fireEvent.input(ttl, { target: { value: '14' } });

		const saveBtn = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(saveBtn);

		expect(putSpy).toHaveBeenCalledWith('/requests/settings', {
			body: {
				voting_mode: 'net_score',
				allow_viewer_votes: false,
				visibility: 'private_to_requester_and_admin',
				voting_ttl_days: 14
			}
		});
	});

	it('disables controls and hides save button for viewer', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet({
			'/requests/settings': defaultSettings
		});
		renderWithQuery(RequestsSettingsPage);

		const votingMode = (await screen.findByLabelText(
			m.requests_settings_voting_mode()
		)) as HTMLSelectElement;
		const allowViewer = screen.getByLabelText(
			m.requests_settings_allow_viewer_votes()
		) as HTMLInputElement;
		const visibility = screen.getByLabelText(m.requests_settings_visibility()) as HTMLSelectElement;
		const ttl = screen.getByLabelText(m.requests_settings_voting_ttl_days()) as HTMLInputElement;

		expect(votingMode.disabled).toBe(true);
		expect(allowViewer.disabled).toBe(true);
		expect(visibility.disabled).toBe(true);
		expect(ttl.disabled).toBe(true);
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
		expect(screen.getByText(m.requests_settings_readonly_note())).toBeTruthy();
	});
});
