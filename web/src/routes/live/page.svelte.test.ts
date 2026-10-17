// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery } from '#lib/testing/harness.js';
import LivePage from './+page.svelte';

const { MockEventSource } = vi.hoisted(() => {
	class MockEventSource {
		static instances: MockEventSource[] = [];
		url: string;
		init?: { withCredentials?: boolean };
		private listeners: Record<string, ((event: unknown) => void)[]> = {};

		constructor(url: string, init?: { withCredentials?: boolean }) {
			this.url = url;
			this.init = init;
			MockEventSource.instances.push(this);
		}

		addEventListener(type: string, handler: (event: unknown) => void) {
			this.listeners[type] = this.listeners[type] ?? [];
			this.listeners[type].push(handler);
		}

		removeEventListener(type: string, handler: (event: unknown) => void) {
			this.listeners[type] = (this.listeners[type] ?? []).filter((h) => h !== handler);
		}

		close() {}

		emit(type: string, event: unknown) {
			for (const handler of [...(this.listeners[type] ?? [])]) {
				handler(event);
			}
		}
	}

	(globalThis as unknown as { EventSource: unknown }).EventSource = MockEventSource;
	return { MockEventSource };
});

describe('live events', () => {
	beforeEach(() => {
		MockEventSource.instances = [];
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it('renders heading, connected status, and event row when message arrives', async () => {
		renderWithQuery(LivePage);
		expect(screen.getByRole('heading', { level: 1, name: m.live_title() })).toBeTruthy();
		const es = MockEventSource.instances[0];
		expect(es).toBeDefined();

		es.emit('open', {});
		expect((await screen.findByRole('status')).textContent).toContain(m.live_connected());

		es.emit('message', {
			type: 'search_dispatched',
			data: JSON.stringify({
				ts: '2026-09-01T12:00:00Z',
				level: 'info',
				title: 'Searching Sonarr',
				detail: '10 items'
			})
		});

		await waitFor(() => {
			expect(screen.getByText('search_dispatched')).toBeTruthy();
		});
		expect(screen.getByText('Searching Sonarr')).toBeTruthy();
		expect(screen.getByText('10 items')).toBeTruthy();
	});

	it('shows disconnected status when stream encounters an error', async () => {
		renderWithQuery(LivePage);
		const es = MockEventSource.instances[0];
		expect(es).toBeDefined();

		es.emit('open', {});
		expect((await screen.findByRole('status')).textContent).toContain(m.live_connected());

		es.emit('error', new Error('stream disconnected'));
		expect((await screen.findByRole('status')).textContent).toContain(m.live_disconnected());
	});

	it('renders empty log before any events arrive', async () => {
		renderWithQuery(LivePage);
		const log = await screen.findByRole('log');
		expect(within(log).queryAllByRole('listitem')).toHaveLength(0);
	});
});
