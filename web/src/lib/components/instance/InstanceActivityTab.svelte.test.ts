// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import InstanceActivityTab from './InstanceActivityTab.svelte';

describe('InstanceActivityTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders preview panel and run list components', async () => {
		stubGet({
			'/runs': []
		});
		renderWithQuery(InstanceActivityTab, { id: forkInstance.id });

		expect(await screen.findByRole('heading', { level: 2, name: m.preview_title() })).toBeTruthy();
		expect(await screen.findByText(m.runs_empty())).toBeTruthy();
	});
});
