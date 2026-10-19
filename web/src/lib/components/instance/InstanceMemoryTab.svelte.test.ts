// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import InstanceMemoryTab from './InstanceMemoryTab.svelte';

describe('InstanceMemoryTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders reset memory component for the instance', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceMemoryTab, { id: forkInstance.id });

		expect(await screen.findByRole('heading', { level: 2, name: m.memory_title() })).toBeTruthy();
		expect(screen.getByRole('button', { name: m.memory_reset_one() })).toBeTruthy();
	});
});
