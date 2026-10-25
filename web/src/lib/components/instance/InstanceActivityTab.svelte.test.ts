// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import '#lib/testing/sse.js';
import InstanceActivityTab from './InstanceActivityTab.svelte';

describe('InstanceActivityTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders preview panel, run list and activity feed', async () => {
		stubGet({
			'/runs': [],
			'/events': [],
			'/instances': [forkInstance],
			'/schedules': [],
			'/system/status': {
				name: 'SnatchArr',
				version: '0.1.0',
				commit: 'abc1234',
				build_date: '2026-10-20',
				profile: 'dev' as const,
				setup_complete: true
			}
		});
		renderWithQuery(InstanceActivityTab, { id: forkInstance.id });

		expect(await screen.findByRole('heading', { level: 2, name: m.preview_title() })).toBeTruthy();
		expect(await screen.findByText(m.runs_empty())).toBeTruthy();
		expect(await screen.findByRole('log')).toBeTruthy();
	});
});
