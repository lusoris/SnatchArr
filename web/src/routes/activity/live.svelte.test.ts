// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import { MockEventSource } from '#lib/testing/sse.js';
import LiveTab from './LiveTab.svelte';

type Event = components['schemas']['Event'];
type Instance = components['schemas']['Instance'];
type Schedule = components['schemas']['Schedule'];
type SystemStatus = components['schemas']['SystemStatus'];

const sampleEvent: Event = {
	id: 101,
	instance_id: '00000000-0000-0000-0000-000000000001',
	ts: '2026-10-20T12:00:00Z',
	level: 'info',
	type: 'search_dispatched',
	title: 'Searching Sonarr',
	detail: '10 items'
};

const sampleInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Sonarr Main',
	kind: 'sonarr',
	base_url: 'http://localhost:8989',
	enabled: true,
	source: 'manual',
	created_at: '2026-10-01T00:00:00Z',
	updated_at: '2026-10-01T00:00:00Z'
};

const sampleSchedule: Schedule = {
	id: '00000000-0000-0000-0000-000000000002',
	instance_id: sampleInstance.id,
	name: 'Nightly Run',
	days: [1, 2, 3, 4, 5],
	start: '02:00',
	end: '04:00',
	tz: 'UTC',
	action: 'pause',
	enabled: true,
	active: true,
	created_at: '2026-10-01T00:00:00Z',
	updated_at: '2026-10-01T00:00:00Z'
};

const sampleStatus: SystemStatus = {
	name: 'SnatchArr',
	version: '0.1.0',
	commit: 'abc1234',
	build_date: '2026-10-20',
	profile: 'dev',
	setup_complete: true,
	worker_last_contact_at: '2026-10-20T12:30:00Z'
};

describe('live events', () => {
	function openMockStream(): MockEventSource {
		const es = MockEventSource.instances[0];
		expect(es).toBeDefined();
		es.emit('open', {});
		return es;
	}

	async function renderAndOpenLiveStream(): Promise<MockEventSource> {
		renderWithQuery(LiveTab);
		const es = openMockStream();
		expect((await screen.findByRole('status')).textContent).toContain(m.live_connected());
		return es;
	}

	async function renderLiveWithBacklog(events: Event[] = [sampleEvent]) {
		stubGet({
			'/events': events,
			'/instances': [sampleInstance],
			'/schedules': [],
			'/system/status': sampleStatus
		});
		renderWithQuery(LiveTab);
		return await screen.findByRole('log');
	}

	beforeEach(() => {
		MockEventSource.instances = [];
		stubGet({
			'/events': [],
			'/instances': [],
			'/schedules': [],
			'/system/status': sampleStatus
		});
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it('renders heading, connected status, and event row when message arrives', async () => {
		const es = await renderAndOpenLiveStream();
		expect(screen.getByRole('heading', { level: 2, name: m.live_title() })).toBeTruthy();

		es.emit('message', {
			type: 'search_dispatched',
			data: JSON.stringify({
				id: 999,
				ts: '2026-10-20T12:00:00Z',
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
		const es = await renderAndOpenLiveStream();

		es.emit('error', new Error('stream disconnected'));
		expect((await screen.findByRole('status')).textContent).toContain(m.live_disconnected());
	});

	it('renders empty state when backlog is empty and stream has no events', async () => {
		stubGet({
			'/events': [],
			'/instances': [sampleInstance],
			'/schedules': [sampleSchedule],
			'/system/status': sampleStatus
		});
		renderWithQuery(LiveTab);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.live_empty_title() })
		).toBeTruthy();
		expect(screen.getByText(m.live_empty_desc())).toBeTruthy();
		expect(screen.getByText(m.live_worker_label())).toBeTruthy();
		expect(screen.getByText(m.live_schedules_label())).toBeTruthy();
		expect(screen.getByText('Sonarr Main')).toBeTruthy();
		expect(screen.getByText(m.live_cycle_paused())).toBeTruthy();
		expect(screen.getByRole('link', { name: m.live_link_history() })).toBeTruthy();
		expect(screen.getByRole('link', { name: m.live_link_schedules() })).toBeTruthy();
	});

	it('renders backlog events fetched on mount', async () => {
		const log = await renderLiveWithBacklog();
		expect(log).toBeTruthy();
		expect(await screen.findByText('search_dispatched')).toBeTruthy();
		expect(screen.getByText('Searching Sonarr')).toBeTruthy();
		expect(screen.getByText('Sonarr Main')).toBeTruthy();
	});

	it('renders backlog error alert when backlog fetch fails, while keeping stream active', async () => {
		stubGet({
			'/events': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'backlog temporarily unavailable'
			}),
			'/instances': [],
			'/schedules': [],
			'/system/status': sampleStatus
		});
		renderWithQuery(LiveTab);

		expect(await screen.findByRole('alert')).toBeTruthy();
		expect(screen.getByText(m.live_backlog_error())).toBeTruthy();

		// Stream still receives events
		const es = openMockStream();
		es.emit('message', {
			type: 'run_queued',
			data: JSON.stringify({
				id: 555,
				ts: '2026-10-20T12:05:00Z',
				level: 'info',
				title: 'Run queued'
			})
		});

		expect(await screen.findByText('run_queued')).toBeTruthy();
		expect(screen.getByText('Run queued')).toBeTruthy();
	});

	it('deduplicates backlog and stream events by id', async () => {
		const log = await renderLiveWithBacklog();
		expect(await screen.findByText('Searching Sonarr')).toBeTruthy();
		expect(within(log).getAllByRole('listitem')).toHaveLength(1);

		const es = openMockStream();

		// Emit frame with identical id 101
		es.emit('message', {
			type: 'search_dispatched',
			data: JSON.stringify({
				id: 101,
				ts: '2026-10-20T12:00:00Z',
				level: 'info',
				title: 'Searching Sonarr Duplicate'
			})
		});

		// Also emit frame with new id 102
		es.emit('message', {
			type: 'search_finished',
			data: JSON.stringify({
				id: 102,
				ts: '2026-10-20T12:01:00Z',
				level: 'info',
				title: 'Search completed'
			})
		});

		await waitFor(() => {
			expect(screen.getByText('search_finished')).toBeTruthy();
		});

		// Count items in log: should be 2 (id 101 deduped, id 102 added)
		expect(within(log).getAllByRole('listitem')).toHaveLength(2);
	});
});
