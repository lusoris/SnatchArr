// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import {
	assertCancelDelete,
	expectStackedLabels,
	renderWithQuery,
	stubGet
} from '#lib/testing/harness.js';
import NotificationsTab from './NotificationsTab.svelte';

type NotificationChannel = components['schemas']['NotificationChannel'];
type NotificationRule = components['schemas']['NotificationRule'];

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

/** The table row that names the record and carries its actions. */
async function actionRow(name: string): Promise<ReturnType<typeof within>> {
	await screen.findAllByText(name);
	const row = screen
		.getAllByRole('row')
		.find((r) => r.textContent?.includes(name) && within(r).queryAllByRole('button').length > 0);
	if (!row) throw new Error(`no action row for ${name}`);
	return within(row);
}

/** Presses Delete, checks nothing was sent, then confirms and returns the spy. */
async function deleteAfterConfirm(name: string): Promise<ReturnType<typeof vi.spyOn>> {
	const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
	const row = await actionRow(name);
	await fireEvent.click(row.getByRole('button', { name: m.action_delete() }));
	expect(deleteSpy).not.toHaveBeenCalled();
	const question = row.getByRole('group', { name: m.confirm_delete({ name }) });
	await fireEvent.click(within(question).getByRole('button', { name: m.confirm_delete_yes() }));
	return deleteSpy;
}

const baseStubs = {
	'/notifications/channels': [],
	'/notifications/rules': []
};

describe('NotificationsTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders notification channels and rules', async () => {
		stubGet({
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': [sampleRule]
		});
		renderWithQuery(NotificationsTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.notifications_title() })
		).toBeTruthy();
		expect(screen.getAllByText('Discord Alerts').length).toBeGreaterThanOrEqual(1);
		expect(screen.getByText('Streak Rule')).toBeTruthy();
		expect(screen.getByRole('button', { name: m.channel_test() })).toBeTruthy();
	});

	it('deletes a channel only after the in-page confirmation', async () => {
		stubGet({ '/notifications/channels': [sampleChannel], '/notifications/rules': [] });
		renderWithQuery(NotificationsTab);

		const deleteSpy = await deleteAfterConfirm(sampleChannel.name);
		expect(deleteSpy).toHaveBeenCalledWith('/notifications/channels/{channelId}', {
			params: { path: { channelId: sampleChannel.id } }
		});
	});

	it('deletes a rule only after the in-page confirmation', async () => {
		stubGet({ '/notifications/channels': [], '/notifications/rules': [sampleRule] });
		renderWithQuery(NotificationsTab);

		const deleteSpy = await deleteAfterConfirm(sampleRule.name);
		expect(deleteSpy).toHaveBeenCalledWith('/notifications/rules/{ruleId}', {
			params: { path: { ruleId: sampleRule.id } }
		});
	});

	it('keeps a channel and a rule when the delete question is cancelled', async () => {
		stubGet({ '/notifications/channels': [sampleChannel], '/notifications/rules': [sampleRule] });
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(NotificationsTab);

		await assertCancelDelete(await actionRow(sampleChannel.name), deleteSpy);
		await assertCancelDelete(await actionRow(sampleRule.name), deleteSpy);
	});

	it('tests notification channel when test button clicked', async () => {
		stubGet({
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': []
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(NotificationsTab);

		const testBtn = await screen.findByRole('button', { name: m.channel_test() });
		await fireEvent.click(testBtn);

		expect(postSpy).toHaveBeenCalledWith('/notifications/channels/{channelId}/test', {
			params: { path: { channelId: sampleChannel.id } }
		});
	});

	it('sends an Apprise channel with its notify URL, Basic Auth and trimmed tags', async () => {
		stubGet(baseStubs);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: sampleChannel } as never);
		renderWithQuery(NotificationsTab);

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
		stubGet(baseStubs);
		renderWithQuery(NotificationsTab);

		await screen.findByRole('heading', { name: m.channel_add() });
		expect(screen.queryByLabelText(m.channel_apprise_url())).toBeNull();
		expect(screen.queryByLabelText(m.channel_tags())).toBeNull();
	});

	it('stacks notification channels table rows below md with data-label matching th on every td except actions', async () => {
		stubGet({
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': []
		});
		renderWithQuery(NotificationsTab);

		const table = await screen.findByRole('table', { name: m.notifications_channels_title() });
		expectStackedLabels(table);
	});

	it('renders no notification channels table rows when channels list is empty', async () => {
		stubGet({
			'/notifications/channels': [],
			'/notifications/rules': []
		});
		renderWithQuery(NotificationsTab);

		expect(await screen.findByText(m.notifications_empty_channels())).toBeTruthy();
		expect(screen.queryByRole('table', { name: m.notifications_channels_title() })).toBeNull();
	});

	it('stacks notification rules table rows below md with data-label matching th on every td except actions', async () => {
		stubGet({
			'/notifications/channels': [sampleChannel],
			'/notifications/rules': [sampleRule]
		});
		renderWithQuery(NotificationsTab);

		const table = await screen.findByRole('table', { name: m.notifications_rules_title() });
		expectStackedLabels(table);
	});

	it('renders no notification rules table rows when rules list is empty', async () => {
		stubGet({
			'/notifications/channels': [],
			'/notifications/rules': []
		});
		renderWithQuery(NotificationsTab);

		expect(await screen.findByText(m.notifications_empty_rules())).toBeTruthy();
		expect(screen.queryByRole('table', { name: m.notifications_rules_title() })).toBeNull();
	});
});
