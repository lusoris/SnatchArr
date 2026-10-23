// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import InstanceQueryHarness from './InstanceQueryHarness.svelte';

describe('instanceQuery', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: loads instance data with a static id string', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceQueryHarness, { id: forkInstance.id });

		expect(await screen.findByText(`loaded: ${forkInstance.name}`)).toBeTruthy();
	});

	it('negative: enters error state when endpoint fails', async () => {
		stubGet({
			'/instances/{instanceId}': new Error('network timeout')
		});
		renderWithQuery(InstanceQueryHarness, { id: forkInstance.id });

		expect(await screen.findByText('error: network timeout')).toBeTruthy();
	});

	it('boundary: resolves data when id is supplied as a getter function', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceQueryHarness, { id: () => forkInstance.id });

		expect(await screen.findByText(`loaded: ${forkInstance.name}`)).toBeTruthy();
	});
});
