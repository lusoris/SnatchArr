// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import InstanceQueryHarness from './InstanceQueryHarness.svelte';

/** Waits for the harness to reach a state and returns the text it shows there. */
async function settled(state: 'loaded' | 'error'): Promise<string | null> {
	await waitFor(() => expect(screen.getByTestId('status').dataset.state).toBe(state));
	return screen.getByTestId('status').textContent;
}

describe('instanceQuery', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: loads instance data with a static id string', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceQueryHarness, { id: forkInstance.id });

		expect(await settled('loaded')).toBe(forkInstance.name);
	});

	it('negative: enters error state when endpoint fails', async () => {
		stubGet({
			'/instances/{instanceId}': new Error('network timeout')
		});
		renderWithQuery(InstanceQueryHarness, { id: forkInstance.id });

		expect(await settled('error')).toBe('network timeout');
	});

	it('boundary: resolves data when id is supplied as a getter function', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceQueryHarness, { id: () => forkInstance.id });

		expect(await settled('loaded')).toBe(forkInstance.name);
	});
});
