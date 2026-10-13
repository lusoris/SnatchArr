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
import SettingsPage from './+page.svelte';

type Settings = components['schemas']['Settings'];

const sampleSettings: Settings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100
};

describe('settings', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading and form with settings from API', async () => {
		stubGet({ '/settings': sampleSettings });
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
		stubGet({ '/settings': sampleSettings });
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
				global_hourly_cap: 100
			}
		});
	});

	it('prevents submission when retention is outside allowed bounds', async () => {
		stubGet({ '/settings': sampleSettings });
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
});
