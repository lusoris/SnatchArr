// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, sampleQueuePolicy, stubGet } from '#lib/testing/harness.js';
import InstanceQueueTab from './InstanceQueueTab.svelte';

describe('InstanceQueueTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders queue policy and strikes components', async () => {
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy,
			'/instances/{instanceId}/queue-strikes': []
		});
		renderWithQuery(InstanceQueueTab, { id: forkInstance.id });

		expect(
			await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.queue_strikes_title() })).toBeTruthy();
	});
});
