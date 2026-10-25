// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import GeneralTab from './GeneralTab.svelte';

type Settings = components['schemas']['Settings'];
type PriorityPreset = components['schemas']['PriorityPreset'];

const mediaFirstRanks = {
	sonarr: 10,
	radarr: 10,
	lidarr: 20,
	readarr: 30,
	whisparr_v2: 40,
	whisparr_v3: 40
};

const samplePresets: PriorityPreset[] = [
	{
		name: 'media_first',
		search: { ...mediaFirstRanks },
		download: { ...mediaFirstRanks }
	}
];

const sampleSettings: Settings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	search_priority_preset: { ...mediaFirstRanks },
	download_priority_preset: { ...mediaFirstRanks },
	llm_enabled: false,
	llm_write_tools: []
};

const baseStubs = {
	'/settings': sampleSettings,
	'/priority-presets': samplePresets
};

function expectedSettingsWithTools(tools: Settings['llm_write_tools']): Settings {
	return {
		...sampleSettings,
		llm_enabled: true,
		llm_write_tools: tools
	};
}

describe('settings', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading and form with settings from API', async () => {
		stubGet(baseStubs);
		renderWithQuery(GeneralTab);

		expect(await screen.findByRole('heading', { level: 2, name: m.settings_title() })).toBeTruthy();

		const retention = (await screen.findByLabelText(m.settings_retention())) as HTMLInputElement;
		const ua = screen.getByLabelText(m.settings_user_agent()) as HTMLInputElement;

		expect(retention.value).toBe('30');
		expect(ua.value).toBe('SnatchArr/1.0');
		expect(screen.queryByLabelText(m.settings_global_cap())).toBeNull();
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
		renderWithQuery(GeneralTab);

		expect((await screen.findByRole('alert')).textContent).toContain('failed to read settings');
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('submits updated settings when save button is clicked', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { ...sampleSettings, history_retention_days: 60 }
		} as never);
		renderWithQuery(GeneralTab);

		const retention = await screen.findByLabelText(m.settings_retention());
		await fireEvent.input(retention, { target: { value: '60' } });

		const button = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(button);

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: {
				history_retention_days: 60,
				user_agent: 'SnatchArr/1.0',
				global_hourly_cap: 100,
				search_priority_preset: mediaFirstRanks,
				download_priority_preset: mediaFirstRanks,
				llm_enabled: false,
				llm_write_tools: []
			}
		});
	});

	it('refuses to save a write tool the form does not know', async () => {
		stubGet({
			...baseStubs,
			'/settings': { ...sampleSettings, llm_write_tools: ['bogus_tool' as never] }
		});
		const putSpy = vi.spyOn(api, 'PUT');
		renderWithQuery(GeneralTab);

		await screen.findByLabelText(m.settings_retention());
		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));

		expect(putSpy).not.toHaveBeenCalled();
	});

	it('disables llm_write_tools checkboxes while llm_enabled is off and submits toggled tools when enabled', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: ['snatcharr_trigger_run', 'snatcharr_delete_instance']
			}
		} as never);
		renderWithQuery(GeneralTab);

		const triggerBox = (await screen.findByLabelText(
			m.settings_llm_tool_trigger_run()
		)) as HTMLInputElement;
		const deleteBox = screen.getByLabelText(
			m.settings_llm_tool_delete_instance()
		) as HTMLInputElement;
		expect(triggerBox.disabled).toBe(true);
		expect(deleteBox.disabled).toBe(true);

		const llmToggle = screen.getByLabelText(m.settings_llm_enabled()) as HTMLInputElement;
		await fireEvent.click(llmToggle);
		expect(triggerBox.disabled).toBe(false);

		await fireEvent.click(triggerBox);
		await fireEvent.click(deleteBox);
		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: expectedSettingsWithTools(['snatcharr_trigger_run', 'snatcharr_delete_instance'])
		});
	});

	it('groups write tools into scope fieldsets with legends and allows toggling across scopes', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: [
					'snatcharr_create_instance',
					'snatcharr_update_settings',
					'snatcharr_create_download_client',
					'snatcharr_snatch_seerr_request',
					'snatcharr_create_prowlarr_link',
					'snatcharr_create_notification_channel'
				]
			}
		} as never);
		renderWithQuery(GeneralTab);

		expect(
			await screen.findByRole('group', { name: m.settings_llm_scope_core_write() })
		).toBeTruthy();
		expect(screen.getByRole('group', { name: m.settings_llm_scope_admin_write() })).toBeTruthy();
		expect(
			screen.getByRole('group', { name: m.settings_llm_scope_download_clients_write() })
		).toBeTruthy();
		expect(screen.getByRole('group', { name: m.settings_llm_scope_requests_write() })).toBeTruthy();
		expect(screen.getByRole('group', { name: m.settings_llm_scope_prowlarr_write() })).toBeTruthy();
		expect(
			screen.getByRole('group', { name: m.settings_llm_scope_notifications_write() })
		).toBeTruthy();

		const llmToggle = screen.getByLabelText(m.settings_llm_enabled()) as HTMLInputElement;
		await fireEvent.click(llmToggle);

		const boxes = [
			screen.getByLabelText(m.settings_llm_tool_create_instance()) as HTMLInputElement,
			screen.getByLabelText(m.settings_llm_tool_update_settings()) as HTMLInputElement,
			screen.getByLabelText(m.settings_llm_tool_create_download_client()) as HTMLInputElement,
			screen.getByLabelText(m.settings_llm_tool_snatch_seerr_request()) as HTMLInputElement,
			screen.getByLabelText(m.settings_llm_tool_create_prowlarr_link()) as HTMLInputElement,
			screen.getByLabelText(m.settings_llm_tool_create_notification_channel()) as HTMLInputElement
		];

		for (const box of boxes) {
			expect(box.disabled).toBe(false);
			await fireEvent.click(box);
		}
		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: expectedSettingsWithTools([
				'snatcharr_create_instance',
				'snatcharr_update_settings',
				'snatcharr_create_download_client',
				'snatcharr_snatch_seerr_request',
				'snatcharr_create_prowlarr_link',
				'snatcharr_create_notification_channel'
			])
		});
	});

	it('prevents submission when retention is outside allowed bounds', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT');
		renderWithQuery(GeneralTab);

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
		renderWithQuery(GeneralTab);

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
		renderWithQuery(GeneralTab);

		const currentPass = await screen.findByLabelText(m.current_password());
		const newPass = screen.getByLabelText(m.new_password());
		const submitBtn = screen.getByRole('button', { name: m.change_password_submit() });

		await fireEvent.input(currentPass, { target: { value: 'wrong-password' } });
		await fireEvent.input(newPass, { target: { value: 'new-valid-password-123' } });
		await fireEvent.click(submitBtn);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('invalid current password');
	});

	it('updates disclosure summary count when tool checkboxes are toggled', async () => {
		stubGet({
			...baseStubs,
			'/settings': {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: []
			}
		});
		renderWithQuery(GeneralTab);

		expect(
			await screen.findByText(m.settings_llm_write_tools_allowed({ count: 0, total: 16 }))
		).toBeTruthy();

		const triggerBox = screen.getByLabelText(m.settings_llm_tool_trigger_run()) as HTMLInputElement;
		await fireEvent.click(triggerBox);

		expect(
			screen.getByText(m.settings_llm_write_tools_allowed({ count: 1, total: 16 }))
		).toBeTruthy();

		const deleteBox = screen.getByLabelText(
			m.settings_llm_tool_delete_instance()
		) as HTMLInputElement;
		await fireEvent.click(deleteBox);

		expect(
			screen.getByText(m.settings_llm_write_tools_allowed({ count: 2, total: 16 }))
		).toBeTruthy();
	});

	it('submits checked tools from closed disclosures when saved', async () => {
		stubGet({
			...baseStubs,
			'/settings': {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: ['snatcharr_trigger_run', 'snatcharr_update_settings']
			}
		});
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: ['snatcharr_trigger_run', 'snatcharr_update_settings']
			}
		} as never);
		renderWithQuery(GeneralTab);

		const coreFieldset = await screen.findByRole('group', {
			name: m.settings_llm_scope_core_write()
		});
		const coreDetails = coreFieldset.closest('details');
		expect(coreDetails).toBeTruthy();
		expect(coreDetails?.open).toBe(false);

		const button = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(button);

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: expectedSettingsWithTools(['snatcharr_trigger_run', 'snatcharr_update_settings'])
		});
	});

	it('renders boundary 0 of n and n of n labels for tool disclosures', async () => {
		const allCoreTools = [
			'snatcharr_create_instance',
			'snatcharr_update_instance',
			'snatcharr_delete_instance',
			'snatcharr_test_instance',
			'snatcharr_test_instance_input',
			'snatcharr_delete_instance_events',
			'snatcharr_update_policy',
			'snatcharr_update_queue_policy',
			'snatcharr_trigger_run',
			'snatcharr_preview_run',
			'snatcharr_cancel_run',
			'snatcharr_reset_state',
			'snatcharr_create_schedule',
			'snatcharr_update_schedule',
			'snatcharr_delete_schedule',
			'snatcharr_import_configarr'
		];
		stubGet({
			...baseStubs,
			'/settings': {
				...sampleSettings,
				llm_enabled: true,
				llm_write_tools: allCoreTools
			}
		});
		renderWithQuery(GeneralTab);

		expect(
			await screen.findByText(m.settings_llm_write_tools_allowed({ count: 16, total: 16 }))
		).toBeTruthy();

		expect(
			screen.getByText(m.settings_llm_write_tools_allowed({ count: 0, total: 1 }))
		).toBeTruthy();
	});
});
