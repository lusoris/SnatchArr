// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { fireEvent, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet, useAdminSession } from '#lib/testing/harness.js';
import EnvironmentTab from './EnvironmentTab.svelte';

type RuntimeSetting = components['schemas']['RuntimeSetting'];

const sampleSettings: RuntimeSetting[] = [
	{
		key: 'port',
		env: 'APP_PORT',
		class: 'bootstrap',
		type: 'int',
		default: '8080',
		value: '8080',
		source: 'default',
		locked: true,
		apply: 'restart'
	},
	{
		key: 'log_level',
		env: 'APP_LOG_LEVEL',
		class: 'runtime',
		type: 'string',
		default: 'info',
		value: 'info',
		source: 'db',
		locked: false,
		apply: 'live'
	},
	{
		key: 'worker_concurrency',
		env: 'APP_WORKER_CONCURRENCY',
		class: 'runtime',
		type: 'int',
		default: '4',
		value: '4',
		source: 'env',
		locked: true,
		apply: 'restart'
	},
	{
		key: 'api_token',
		env: 'APP_API_TOKEN',
		class: 'secret',
		type: 'string',
		default: '',
		value: 'fake-api-key-0123456789', // PLANTED SECRET VALUE to verify it's never in DOM!
		is_set: true,
		source: 'db',
		locked: false,
		apply: 'restart'
	}
];

describe('EnvironmentTab positive', () => {
	useAdminSession();

	it('renders rows grouped by class with env, key, source, and values', async () => {
		stubGet({ '/settings/runtime': sampleSettings });
		renderWithQuery(EnvironmentTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.environment_title() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: m.environment_class_bootstrap() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: m.environment_class_runtime() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: m.environment_class_secret() })
		).toBeTruthy();

		expect(screen.getByText('APP_PORT')).toBeTruthy();
		expect(screen.getByText('APP_LOG_LEVEL')).toBeTruthy();
		expect(screen.getByText('APP_WORKER_CONCURRENCY')).toBeTruthy();
		expect(screen.getByText('APP_API_TOKEN')).toBeTruthy();

		expect(screen.getByRole('button', { name: m.instance_save() })).toBeTruthy();
	});

	it('edit runtime row + save sends only changed key; restart notice shown', async () => {
		stubGet({ '/settings/runtime': sampleSettings });
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: {
				pending_restart: true,
				settings: sampleSettings.map((s) => (s.key === 'log_level' ? { ...s, value: 'debug' } : s))
			}
		} as never);

		renderWithQuery(EnvironmentTab);

		const input = (await screen.findByLabelText('log_level')) as HTMLInputElement;
		expect(input.value).toBe('info');
		await fireEvent.input(input, { target: { value: 'debug' } });

		const saveBtn = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(saveBtn);

		expect(putSpy).toHaveBeenCalledWith('/settings/runtime', {
			body: { values: { log_level: 'debug' } }
		});

		const notice = await screen.findByRole('status');
		expect(notice.textContent).toContain(m.environment_restart_notice());
	});
});

describe('EnvironmentTab negative', () => {
	useAdminSession();

	it('locked row input disabled with reason; secret value never rendered; 409 problem shown via ProblemNote', async () => {
		stubGet({ '/settings/runtime': sampleSettings });
		renderWithQuery(EnvironmentTab);

		// 1. Locked row input disabled with reason
		const lockedInput = (await screen.findByLabelText('worker_concurrency')) as HTMLInputElement;
		expect(lockedInput.disabled).toBe(true);
		expect(
			screen.getByText(m.environment_locked_by_env({ env: 'APP_WORKER_CONCURRENCY' }))
		).toBeTruthy();

		const bootstrapInput = screen.getByLabelText('port') as HTMLInputElement;
		expect(bootstrapInput.disabled).toBe(true);
		expect(screen.getByText(m.environment_bootstrap_reason())).toBeTruthy();

		// 2. Secret value never rendered (planted value 'fake-api-key-0123456789' must not appear in DOM)
		expect(document.body.textContent).not.toContain('fake-api-key-0123456789');
		expect(document.body.innerHTML).not.toContain('fake-api-key-0123456789');
		expect(screen.getByText(m.environment_secret_set())).toBeTruthy();

		// 3. 409 problem shown via ProblemNote
		vi.spyOn(api, 'PUT').mockRejectedValue(
			new ProblemError({
				type: 'https://tools.ietf.org/html/rfc9457',
				title: 'Conflict',
				status: 409,
				detail: 'Setting is locked by environment'
			})
		);

		const runtimeInput = screen.getByLabelText('log_level');
		await fireEvent.input(runtimeInput, { target: { value: 'debug' } });

		const saveBtn = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(saveBtn);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Setting is locked by environment');
	});
});

describe('EnvironmentTab boundary', () => {
	it('empty list renders empty message', async () => {
		useAdminSession();
		stubGet({ '/settings/runtime': [] });
		renderWithQuery(EnvironmentTab);

		expect(await screen.findByText(m.environment_empty())).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('asks a viewer for no admin-only settings and offers no form', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		const get = stubGet({});
		renderWithQuery(EnvironmentTab);

		expect(await screen.findByText(m.environment_admin_only())).toBeTruthy();
		expect(get.mock.calls.map((c) => c[0])).not.toContain('/settings/runtime');
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('treats a requester, the highest role below admin, as a viewer', async () => {
		session.user = { id: 'u3', username: 'requester', role: 'requester' } as never;
		const get = stubGet({});
		renderWithQuery(EnvironmentTab);

		expect(await screen.findByText(m.environment_admin_only())).toBeTruthy();
		expect(get.mock.calls.map((c) => c[0])).not.toContain('/settings/runtime');
	});

	it('asks for the settings once a viewer becomes admin', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		const get = stubGet({ '/settings/runtime': sampleSettings });
		renderWithQuery(EnvironmentTab);
		expect(await screen.findByText(m.environment_admin_only())).toBeTruthy();

		session.user = { id: 'u2', username: 'viewer', role: 'admin' } as never;

		expect(await screen.findByText('APP_PORT')).toBeTruthy();
		expect(get.mock.calls.map((c) => c[0])).toContain('/settings/runtime');
		expect(screen.queryByText(m.environment_admin_only())).toBeNull();
	});
});

describe('EnvironmentTab unchanged save', () => {
	useAdminSession();

	it('sends no request when nothing changed', async () => {
		stubGet({ '/settings/runtime': sampleSettings });
		const putSpy = vi.spyOn(api, 'PUT');
		renderWithQuery(EnvironmentTab);

		await screen.findByLabelText('log_level');
		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));

		expect(putSpy).not.toHaveBeenCalled();
		expect(screen.queryByRole('status')).toBeNull();
	});
});
