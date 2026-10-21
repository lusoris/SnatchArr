// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SettingsPage from './+page.svelte';

type Settings = components['schemas']['Settings'];
type NotificationChannel = components['schemas']['NotificationChannel'];
type NotificationRule = components['schemas']['NotificationRule'];

const sampleSettings: Settings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	llm_enabled: false
};

const sampleChannel: NotificationChannel = {
	id: '018f2651-3472-749e-8c38-5188bf287c80',
	name: 'Discord Alerts',
	kind: 'discord',
	enabled: true,
	has_secret: true,
	config: {
		allow_private_network: false,
		allow_insecure_http: false
	},
	created_at: '2026-10-16T10:00:00Z',
	updated_at: '2026-10-16T10:00:00Z'
};

const sampleRule: NotificationRule = {
	id: '018f2651-3472-749e-8c38-5188bf287c81',
	name: 'Streak Rule',
	event_kind: 'run_failure_streak',
	threshold: 3,
	enabled: true,
	channel_ids: ['018f2651-3472-749e-8c38-5188bf287c80'],
	created_at: '2026-10-16T10:00:00Z',
	updated_at: '2026-10-16T10:00:00Z'
};

const baseStubs = {
	'/settings': sampleSettings,
	'/notifications/channels': [],
	'/notifications/rules': []
};

describe('settings', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading and form with settings from API', async () => {
		stubGet(baseStubs);
		renderWithQuery(SettingsPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.settings_title() })).toBeTruthy();

		const retention = (await screen.findByLabelText(m.settings_retention())) as HTMLInputElement;
		const ua = screen.getByLabelText(m.settings_user_agent()) as HTMLInputElement;
		const cap = screen.getByLabelText(m.settings_global_cap()) as HTMLInputElement;

		expect(retention.value).toBe('30');
		expect(ua.value).toBe('SnatchArr/1.0');
		expect(cap.value).toBe('100');
		expect(screen.getByRole('button', { name: m.instance_save() })).toBeTruthy();
	});

	it('announces the problem document when the settings query fails', async () => {
		stubGet({
			...baseStubs,
			'/settings': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'failed to read settings'
			})
		});
		renderWithQuery(SettingsPage);

		expect((await screen.findByRole('alert')).textContent).toContain('failed to read settings');
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('submits updated settings when save button is clicked', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { ...sampleSettings, history_retention_days: 60 }
		} as never);
		renderWithQuery(SettingsPage);

		const retention = await screen.findByLabelText(m.settings_retention());
		await fireEvent.input(retention, { target: { value: '60' } });

		const button = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(button);

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: {
				history_retention_days: 60,
				user_agent: 'SnatchArr/1.0',
				global_hourly_cap: 100,
				llm_enabled: false
			}
		});
	});

	it('prevents submission when retention is outside allowed bounds', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT');
		renderWithQuery(SettingsPage);

		const retention = (await screen.findByLabelText(m.settings_retention())) as HTMLInputElement;
		await fireEvent.input(retention, { target: { value: '0' } });

		expect(retention.checkValidity()).toBe(false);
		expect(retention.validity.rangeUnderflow).toBe(true);

		const button = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(button);
		expect(putSpy).not.toHaveBeenCalled();
	});

	it('renders change password form and submits password change', async () => {
		stubGet(baseStubs);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(SettingsPage);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.change_password_title() })
		).toBeTruthy();
		const currentPass = screen.getByLabelText(m.current_password());
		const newPass = screen.getByLabelText(m.new_password());
		const submitBtn = screen.getByRole('button', { name: m.change_password_submit() });

		await fireEvent.input(currentPass, { target: { value: 'current-valid-password' } });
		await fireEvent.input(newPass, { target: { value: 'new-valid-password-123' } });
		await fireEvent.click(submitBtn);

		expect(postSpy).toHaveBeenCalledWith('/auth/password', {
			body: {
				current_password: 'current-valid-password',
				new_password: 'new-valid-password-123'
			}
		});
	});

	it('displays error when change password fails', async () => {
		stubGet(baseStubs);
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({ type: 'about:blank', status: 401, detail: 'invalid current password' })
		);
		renderWithQuery(SettingsPage);

		const currentPass = await screen.findByLabelText(m.current_password());
		const newPass = screen.getByLabelText(m.new_password());
		const submitBtn = screen.getByRole('button', { name: m.change_password_submit() });

		await fireEvent.input(currentPass, { target: { value: 'wrong-password' } });
		await fireEvent.input(newPass, { target: { value: 'new-valid-password-123' } });
		await fireEvent.click(submitBtn);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('invalid current password');
	});

	it('renders notification channels and rules', async () => {
		stubGet({
			'/settings': sampleSettings,
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': [sampleRule]
		});
		renderWithQuery(SettingsPage);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.notifications_title() })
		).toBeTruthy();
		expect(screen.getAllByText('Discord Alerts').length).toBeGreaterThanOrEqual(1);
		expect(screen.getByText('Streak Rule')).toBeTruthy();
		expect(screen.getByRole('button', { name: m.channel_test() })).toBeTruthy();
	});

	it('tests notification channel when test button clicked', async () => {
		stubGet({
			'/settings': sampleSettings,
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': []
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(SettingsPage);

		const testBtn = await screen.findByRole('button', { name: m.channel_test() });
		await fireEvent.click(testBtn);

		expect(postSpy).toHaveBeenCalledWith('/notifications/channels/{channelId}/test', {
			params: { path: { channelId: sampleChannel.id } }
		});
	});
	it('sends an Apprise channel with its notify URL, Basic Auth and trimmed tags', async () => {
		stubGet({
			'/settings': sampleSettings,
			'/notifications/channels': [],
			'/notifications/rules': []
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: sampleChannel } as never);
		renderWithQuery(SettingsPage);

		const heading = await screen.findByRole('heading', { name: m.channel_add() });
		const form = within(heading.closest('form') as HTMLElement);
		await fireEvent.input(form.getByLabelText(m.channel_name()), { target: { value: 'Apprise' } });
		await fireEvent.change(form.getByLabelText(m.channel_kind()), {
			target: { value: 'apprise' }
		});
		const set = async (label: string, value: string) =>
			fireEvent.input(await form.findByLabelText(label), { target: { value } });
		await set(m.channel_apprise_url(), 'http://apprise:8000/notify/snatch-key');
		await set(m.channel_username(), 'snatch');
		await set(m.channel_password(), 'pw');
		await set(m.channel_tags(), ' ops, ,home ');
		await fireEvent.click(form.getByRole('button', { name: m.channel_add() }));

		expect(postSpy).toHaveBeenCalledWith('/notifications/channels', {
			body: expect.objectContaining({
				kind: 'apprise',
				config: expect.objectContaining({
					webhook_url: 'http://apprise:8000/notify/snatch-key',
					username: 'snatch',
					password: 'pw',
					tags: ['ops', 'home']
				})
			})
		});
	});

	it('shows the Apprise fields only for the Apprise kind', async () => {
		stubGet({
			'/settings': sampleSettings,
			'/notifications/channels': [],
			'/notifications/rules': []
		});
		renderWithQuery(SettingsPage);

		await screen.findByRole('heading', { name: m.channel_add() });
		expect(screen.queryByLabelText(m.channel_apprise_url())).toBeNull();
		expect(screen.queryByLabelText(m.channel_tags())).toBeNull();
	});
});
