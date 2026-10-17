// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import {
	forkInstance,
	renderWithQuery,
	sampleQueueStrikes,
	stubGet
} from '#lib/testing/harness.js';
import QueueStrikes from './QueueStrikes.svelte';

describe('QueueStrikes', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders empty state message when no strikes exist', async () => {
		stubGet({
			'/instances/{instanceId}/queue-strikes': []
		});
		renderWithQuery(QueueStrikes, { instanceId: forkInstance.id });

		expect(
			await screen.findByRole('heading', { level: 2, name: m.queue_strikes_title() })
		).toBeTruthy();
		expect(await screen.findByText(m.queue_strikes_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('renders a row for an active strike', async () => {
		stubGet({
			'/instances/{instanceId}/queue-strikes': sampleQueueStrikes
		});
		renderWithQuery(QueueStrikes, { instanceId: forkInstance.id });

		expect(
			await screen.findByRole('heading', { level: 2, name: m.queue_strikes_title() })
		).toBeTruthy();
		expect(await screen.findByText('nzb-test-123')).toBeTruthy();
		expect(screen.getByText(m.queue_rule_stalled())).toBeTruthy();
		expect(screen.getByText('2 / 3')).toBeTruthy();
	});
});
