// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { toast } from 'svelte-sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { queryClient } from '#lib/query.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import ModulesTab from './ModulesTab.svelte';

type ModuleState = components['schemas']['ModuleState'];
type Module = components['schemas']['Module'];

const sampleModules: ModuleState[] = [
	{ id: 'llm', enabled: false },
	{ id: 'watchaware', enabled: true }
];

const sampleModuleDetail: Module = {
	id: 'llm',
	enabled: false,
	config: { model: 'gpt-4o' },
	updated_at: '2026-10-25T00:00:00Z'
};

describe('ModulesTab', () => {
	beforeEach(() => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders module list with status and toggle buttons for admin', async () => {
		stubGet({
			'/system/modules': sampleModules
		});
		renderWithQuery(ModulesTab);

		expect(await screen.findByRole('heading', { level: 2, name: m.modules_title() })).toBeTruthy();
		expect(screen.getByText('llm')).toBeTruthy();
		expect(screen.getByText('watchaware')).toBeTruthy();
		expect(screen.getAllByText(m.modules_status_disabled()).length).toBeGreaterThanOrEqual(1);
		expect(screen.getAllByText(m.modules_status_enabled()).length).toBeGreaterThanOrEqual(1);

		const enableBtn = screen.getByRole('button', { name: m.modules_toggle_enable() });
		expect(enableBtn).toBeTruthy();
		const disableBtn = screen.getByRole('button', { name: m.modules_toggle_disable() });
		expect(disableBtn).toBeTruthy();
	});

	it('renders empty message when no modules exist', async () => {
		stubGet({
			'/system/modules': []
		});
		renderWithQuery(ModulesTab);

		expect(await screen.findByText(m.modules_empty())).toBeTruthy();
	});

	it('hides toggle action buttons for viewer', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet({
			'/system/modules': sampleModules
		});
		renderWithQuery(ModulesTab);

		expect(await screen.findByText('llm')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.modules_toggle_enable() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.modules_toggle_disable() })).toBeNull();
	});

	it('displays problem note when modules query fails', async () => {
		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/system/modules') {
				throw new ProblemError({
					type: 'about:blank',
					title: 'Service Unavailable',
					status: 503,
					detail: 'Module service unavailable'
				});
			}
			return { data: undefined } as never;
		});

		renderWithQuery(ModulesTab);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Module service unavailable');
	});

	it('toggles module enabled via GET then PUT preserving config', async () => {
		stubGet({
			'/system/modules': sampleModules,
			'/system/modules/{id}': sampleModuleDetail
		});

		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { ...sampleModuleDetail, enabled: true }
		} as never);
		const toastSuccessSpy = vi.spyOn(toast, 'success');
		const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

		renderWithQuery(ModulesTab);

		const enableBtn = await screen.findByRole('button', { name: m.modules_toggle_enable() });
		await fireEvent.click(enableBtn);

		expect(putSpy).toHaveBeenCalledWith('/system/modules/{id}', {
			params: { path: { id: 'llm' } },
			body: {
				enabled: true,
				config: { model: 'gpt-4o' }
			}
		});

		expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_toggle_success());
		expect(invalidateSpy).toHaveBeenCalled();
	});

	it('displays problem note when toggle fails', async () => {
		stubGet({
			'/system/modules': sampleModules
		});

		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/system/modules') {
				return { data: sampleModules } as never;
			}
			if (path === '/system/modules/{id}') {
				throw new ProblemError({
					type: 'about:blank',
					title: 'Forbidden',
					status: 403,
					detail: 'Admin privilege required'
				});
			}
			return { data: undefined } as never;
		});

		const toastErrorSpy = vi.spyOn(toast, 'error');

		renderWithQuery(ModulesTab);

		const enableBtn = await screen.findByRole('button', { name: m.modules_toggle_enable() });
		await fireEvent.click(enableBtn);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Admin privilege required');
		expect(toastErrorSpy).toHaveBeenCalled();
	});
});
